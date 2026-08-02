# DNF Robot 全局实现与运行逻辑

本文记录当前仓库的实际实现、关键数据边界、兼容策略和发布验收口径。内容以代码调用链为准，不把历史实验方案当作现行逻辑。

- 文档日期：2026-07-26
- 主程序：`cmd/robot`
- 默认管理端口：Robot TCP `8111`，Web `8112`
- 默认游戏服务端口：Game `10011`，Monitor `30303`，Auction `30803`，Point `30603`，Relay `7200`
- VM 操作约束和路径见 `doc/vm.md`

## 1. 总体架构

程序采用单主进程加 Web 子进程结构。

1. 主进程负责配置、PVF、密钥、数据库、Robot TCP API、DNF 协议会话、Actor 调度、摆摊和市场应用。
2. Web 管理端由主进程监督，以 `--web-admin` 子进程运行；异常退出后自动拉起。
3. 自动机器人由 Supervisor 管理 Actor 容器，每个 Actor 独占一个 UID 租约并串行执行登录、移动、喊话、摆摊和释放。
4. 普通物品摊与分解摊共享总创建并发和点位系统，类型由“当前活跃数 + 正在创建数”自动平衡。
5. 拍卖行和金币寄售由 Market App 统一规划，通过 Auction/Point 原生 TCP 协议执行，不直接伪造最终数据库结果。

主要代码边界：

- `internal/foundation`：配置、日志、锁、SQL、进程和网络基础能力。
- `internal/capability`：角色、装备、PVF、摆摊、市场等领域能力。
- `internal/protocol`：DNF、Auction、Monitor、NoCache 协议实现。
- `internal/actor`：单 UID 状态机和串行命令执行。
- `internal/scheduler`：Actor 监督、自适应调度、资源和结构性操作。
- `internal/entry`：TCP API 与 Web 管理入口。

## 2. 启动流程

主进程启动顺序如下：

1. 根据可执行文件目录定位 `config/config.ini`，缺失时生成默认配置。
2. 初始化 `config/log_robot` 的大小轮转日志。
3. 释放缺失的运行文件，包括 `robot_config.ini`、名字和喊话模板、默认 RSA 密钥。
4. 从 `DfGameR` 同目录的 `Script.pvf` 导出装备、材料、地图、等级经验、技能状态和 `pvf_iteminfo.dat`。
5. 校验 RSA 密钥。密钥无效时保留管理和诊断能力，但阻止创建、登录、移动、喊话、摆摊等游戏业务命令。
6. 根据最大在线数和数据库池大小检查 Linux 文件描述符容量。
7. 启动 Party route-0 UDP sink，并配置机器人账号范围、Relay 端口和 RSA 运行密钥。
8. 打开 MySQL 连接池并执行 Ping；连接失败时主程序拒绝继续启动。
9. 创建 RobotManager、NoCache 客户端、Monitor 客户端和 Market App。
10. 启动 Robot TCP API、Web 子进程 Supervisor、自动 Actor Supervisor 和可选 Market Auto。
11. 收到 SIGINT/SIGTERM 后按 TCP、Web、Market、Actor、DNF Runtime、数据库顺序关闭。

日志输出采用轮转、周期 flush 和关闭时同步落盘。`robot_stdout.log` 必须经 `--bounded-log-sink` 写入，不能用无限增长的直接重定向。

## 3. 配置层

### 3.1 `config.ini`

负责服务和基础设施配置：

- Robot、Web、Game、Monitor、Auction、Point、Relay、PartyRoute0 端口。
- `DfGameR`、连接 IP、内部 IP、GameServerGroup。
- Web 密码。
- MySQL 地址、账号、库名、连接池和超时。
- 日志上限、备份数和最大 API 响应大小。

`DfGameR` 同时用于推导服务根目录：当路径形如 `<root>/game/df_game_r` 时，Auction 和 Point 使用同一个 `<root>`；无法识别的布局才回退 `/home/neople`。因此 `/home/neople` 和 `/home/dxf` 布局共用同一套逻辑。

### 3.2 `robot_config.ini`

公开给用户的配置集中在以下部分：

- `create`：等级、职业、成长类型、UID 范围、命名、初始金币和背包容量。
- `spawn`：固定出生或地图范围内随机出生。
- `equipment`：装备槽位、品级、强化、锻造和套装偏好。
- `avatar`：装扮槽位、最少装扮数和套装偏好。
- `store`：摆摊强化、材料/装备背包起始位置、价格和确认超时。
- `follow`：固定跟随账号和坐标半径。
- `shout`：喊话节奏及是否实际发送。
- `auto`：自动开关和目标在线数。

登录速率、熔断、摆摊并发和动作间隔等细节仍可由旧配置键读取，但运行时会根据目标和健康状态重新计算，不作为主要用户调参面。

### 3.3 `market_config.ini`

负责 Market 实际可调的数据库名、系统卖家/买家 UID、补货、回收、金币寄售、并发、自动周期和 `iteminfo.dat` 同步目标。Auction/Point 地址和端口沿用主 `config.ini`，不在 Market 配置中重复。文件按 INI 分节保存，每个价格与动作限制参数都有独立注释；旧版 `market_config.json` 会在首次启动时自动读取并迁移为 INI。

拍卖价格包含装备基础倍率、随机强化区间、每级强化加价率和最终随机倍率。`equipment_level_min/max` 在生成上架动作前按 PVF 装备等级过滤，任一边界为 `0` 表示不限制对应方向。开启 `custom_price_enabled` 后，`market_item_price_ranges.json` 中有效的物品最终价格范围优先于通用公式；未命中的物品继续使用公式。补货与虚拟买家概率回收共用同一价格范围，范围内使用高回收概率，范围外使用低回收概率。

配置使用临时文件原子替换。单品价格 JSON 不存在时自动生成带字段说明的空模板；文件整体损坏或单条数据无效时记录状态并回退通用公式，不中断 Market。旧版 Market JSON 损坏时保存为 `market_config.json.invalid` 并使用安全默认值生成 INI。

## 4. PVF 与运行文件

程序检查 `Script.pvf` 的路径、大小、修改时间和 MD5。导出有效时生成：

- `pvf_equipment_catalog.json`
- `pvf_stackable_catalog.json`
- `pvf_map_catalog.json`
- `pvf_level_exp_catalog.json`
- `pvf_skill_state_catalog.json`
- `pvf_iteminfo.dat`
- `pvf_manifest.json`

PVF 未变化时直接加载现有导出，避免重复全量解析。目录缓存以绝对路径、修改时间和文件大小为键，文件变化后自动重载。

`pvf_iteminfo.dat` 可同步到 `/home/neople` 和 `/home/dxf` 的 Auction/Point 目录；不存在的目标跳过，存在的目标写入并由状态页报告。

## 5. 机器人创建

一次创建请求最多 200 个角色，并作为结构性操作独占执行；自动调度在创建期间进入 maintenance，避免一边扩容一边改库。

创建链路：

1. 创建或校验 `d_starsky` 机器人表。
2. 校验 UID 保护位，扫描所有相关表，确保机器人 UID 范围不覆盖真实账号或残留数据。
3. 从空闲 UID 和下一个 CID 开始分配身份。
4. 名字按模板生成，并按游戏 20 字节及 Windows-1252 兼容规则校验；可显式启用 ASCII 后备命名。
5. 等级随机后从 PVF 等级经验表写入正确最低经验。
6. 职业会先与 PVF 装扮覆盖能力求交集，无法满足最少装扮槽位的职业不会被创建。
7. 出生地图按等级过滤，并按地图面积平方根做平滑加权分布，降低角色集中在单一地图的概率。
8. 固定出生或跟随账号配置可覆盖随机地图；坐标始终限制在有效地图范围内。
9. 写账号、登录、计费、白名单、代理、角色、角色状态、背包、技能等基础记录。
10. 从 PVF 选择接近角色等级、职业可用的装备，优先完整度更高的套装，再补齐随机槽位。
11. 装扮优先选择覆盖至少 6 槽或配置最低槽位数的套装，再补齐其他槽位。
12. 写世界喇叭、重建 `charac_view`、写 `Dummylist` 并登记 `robot_registry`。

装备实例使用完整 61 字节槽位格式。普通角色装备强化、锻造按配置随机；摆摊装备使用独立的固定强化值和封装标记。

## 6. 登录、Logout 与缓存边界

### 6.1 登录

登录请求先检查单次上限、总在线上限、当前已在线 UID 和系统包速率。每个 UID 在发送前确保世界喇叭存在，然后进入 DNF Runtime 的 Init、Login、Running 状态。

需要确认的登录会轮询 Runtime 状态，只有连接存在、状态为 Running 且无断线原因才计为成功。

### 6.2 Logout + NoCache

Logout 已合并角色缓存失效能力，顺序固定为：

1. 向机器人会话发送 Logout/关闭请求。
2. 使用最新 Runtime 快照确认连接已经消失。
3. 仅在连接确实消失后发送 NoCache 包。
4. NoCache 使用 UDP，目标端口为 Game TCP 端口 `+1000`，opcode 为 `0x1b6d`，携带 UID、GameServerGroup 和 game+monitor 模式。
5. NoCache 发送成功后，Logout 才返回 Closed/Confirmed。

连接仍存在时不会提前清缓存；UDP 发送失败时也不会把 Logout 伪报为成功。这样 Logout 本身就是后续离线写库的基础能力边界。

### 6.3 为什么仍需要离线写库

df_game 会在退出阶段写回最终角色快照。在线直接改背包、村庄、职业或摆摊记录，可能被最后快照覆盖。现行逻辑不猜测“落库时间”，而是：

1. 先关闭角色/账号会话。
2. 确认离线。
3. 执行数据库写入。
4. 主动 NoCache。
5. 再登录或重选角色。

这消除了依赖长等待时间、固定 sleep 或反复硬重连的核心矛盾。

## 7. Actor 与自动调度

每个自动 Actor 同时只处理一个 UID。状态包括 Idle、Assigned、Online、Running、Busy、Offline、Releasing 等，UID 租约由 Ledger 保证唯一。

Supervisor 每秒执行：

1. 回收已结束 Actor。
2. 采集在线、连接中、摆摊、CPU、内存、goroutine、端口和熔断信号。
3. 更新调度策略。
4. 检查密钥、结构性操作、Game 端口稳定性和熔断。
5. 扩缩 Actor 容器，释放坏租约和不健康 Actor。
6. 给空闲 Actor 分配可用机器人 UID。
7. 输出周期指标。

关键保护：

- RSA 无效：停止自动 Actor，不发业务包。
- Game 端口不稳定：停止扩容，分批释放 Actor。
- 连接异常比例连续超限：进入熔断，暂停新工作并逐批恢复。
- CPU `>=85%`、内存 `>=4096 MB`、goroutine `>=20000`、连接积压或 Actor 积压：进入 pressure，降低登录和摆摊并发。
- 创建、清理等结构性操作期间：进入 maintenance。

## 8. 在线目标和摊位数量

### 8.1 在线目标

`auto.auto_target_online_count` 是自动在线目标。游戏服务自身的 `max_user_num` 是另一层容量，目标 600 时必须先通过 Web 的 `Max` 按钮把服务容量设置为至少 1000。

### 8.2 摊位目标是自动计算的

当前没有独立的“固定摊位数量”用户配置。活跃摊位目标为：

```text
store_target = min(ceil(auto_target_online_count / 4), 108)
```

示例：

- 在线目标 20：摊位目标 5。
- 在线目标 80：摊位目标 20。
- 在线目标 600：理论值 150，受当前点位打包容量上限限制，实际目标 108。

108 是两类摊位合计，不是单类槽位限制。调度器在在线人数达到目标约 95%、Game 端口正常且资源无压力时提高摆摊概率和创建并发；达到摊位目标后把新增摆摊概率降为 0。

创建并发与最终摊位目标是两回事。两类摊位共享自适应总并发；普通物品摊另有点位证据保护：成功点少于 20 时同时创建最多 8 个，成功点达到 20 后上限按 `success_points/3` 增长，但仍不超过总并发。该限制只控制创建速度，不限制最终活跃物品摊数量。

普通物品摊和分解摊按以下规则选择：

```text
比较 item_active + item_pending 与 disjoint_active + disjoint_pending
数量较少的一类优先；相等时先选普通物品摊
```

因此两类数量会自然波动并趋于平衡，不需要人工设定各自数量。

## 9. 普通物品摊

### 9.1 商品池

启动时从 PVF 构建两个只读池：

- 3 个材料位：仅基础、可交易、非过期、非职业材料的普通材料或怪物卡片。
- 4 个装备位：仅 item type 1..10、可交易、`sealing`、非过期装备。

每个 UID 使用稳定随机种子从池中去重抽取，因此同一版本下商品组合稳定，机器人之间仍有差异。

标准摊位最多 7 件，即 3 材料 + 4 装备。材料数量优先使用 PVF stack limit，缺失或超过 1000 时使用 1000；装备数量固定 1。所有商品总价受 5 亿上限约束。

若 PVF 池不足，逻辑不会伪造重复物品或用 0 填槽，而是“池里有多少摆多少”。所以 7 件是优先目标，不是无条件伪造的硬条件。

### 9.2 写库和开摊顺序

自动普通摊严格执行：

1. 占用 UID busy 标记和共享摆摊并发槽。
2. 从点位协调器领取一个点位。
3. 完整退出账号并确认离线。
4. 写 `Dummylist` 坐标和 `function_type=2`。
5. 同步 `charac_info`、`charac_stat` 的村庄及 `village_prev`，并回读校验。
6. 在一个事务中补齐并校验私人商店权限记录。
7. 在背包 blob 中清理旧摆摊槽，最多写 3 个材料和 4 个封装装备。
8. 用单事务替换 `Robot_stall` 和 `Robot_stall_config`，并校验记录数。
9. 发送 NoCache。
10. 登录并等待 Running。
11. 发送 CMD 88 创建私人商店，再发送显示商品包。
12. 等待 `StoreDisplayAck`，成功后提交点位占用。

数据库记录和游戏背包必须来自同一商品列表，避免摊位表写了 7 件而角色背包没有对应实例。

### 9.3 背包旧快照处理

若 CMD 88 已成功，但商品显示阶段表明角色仍看到旧背包，当前只允许一次原生角色选择刷新：

1. 回到角色选择界面。
2. 在角色离线写边界重新写位置、村庄、背包和摊位表。
3. 再次 NoCache。
4. 重选角色并重试显示。

这里不做第二次完整账号硬登出，避免重新进入 df_game 的账号重连缓存。若刷新后仍失败，则按真实错误结束，不使用长等待或无限重试。

### 9.4 错误分类

- `0x38`：对象注册/点位冲突，可换坐标。
- `0x3e`：区域、状态或商业限制，部分分支与位置有关，允许有限换点。
- `0x52`：明确的商业限制区，标记该点及附近区域为永久不可用。
- `0x11`：商品或背包校验失败，不再当作位置错误。
- `0x3f`：账号私人商店权限与请求不一致，不换点。
- `0x72`：账号或交易安全限制，不换点。
- Runtime 停止、NoCache 失败、写库失败：终止本次会话并清理。

## 10. 分解摊

分解摊 `function_type=3`，费用固定 500 金币，使用职业 3。

首次开摊顺序：

1. 直接关闭当前 Runtime，并确认关闭。
2. 对已关闭会话执行 NoCache。
3. 等待账号离线。
4. 在一个事务中锁定机器人、角色、状态和专家职业记录。
5. 写 `expert_job=3`，专家经验不足时提升到验证过的 800，补齐专家职业信息。
6. 写分解摊位置、`function_type=3`、费用和角色村庄。
7. 再次 NoCache。
8. 登录时把 CMD 238 挂在首次 StateRun 后立即发送，避免调度轮询竞态。
9. 等待 `RobotType=3 && DisjointActive`。

坐标类失败在当前登录会话中执行 SetArea、换点并重发 CMD 238，不再重复完整退出和职业写库。可同会话重试的原因包括 `set_area_failed`、超时、`0x14`、`0x3e`、`0x52`、`0xbe`。

`0x0a`、`0x13`、`0x15`、`0x16`、职业不匹配、Runtime 停止、写库或 NoCache 失败属于结构性失败，立即结束，避免无意义的坐标重试风暴。

## 11. 点位系统

点位来自 `pvf_map_catalog.json`：

- X 步长 120。
- Y 步长 80，但活跃摊位垂直冲突距离按 160 控制。
- 过滤门口、传送区和未验证区域。
- 区域按现场容量经验排序。
- 先使用已成功点和打包后的无冲突点，再探索未知点，最后在冷却后重试部分失败点。

点位状态分三层：

- 内存 claim：防止并发创建拿到同一点。
- 成功 occupancy：在摊位生命周期内防止附近位置复用。
- 历史证据：记录成功、失败、原因和时间，影响后续优先级。

缓存文件：

- `store_points_cache.json`：地图 MD5、点位和成功/失败历史。版本、地图 MD5 或步长变化时自动重建。
- `store_points_active.json`：活跃占用及到期时间，Robot 重启后继续防止与现存摊位重叠。

不建议在活跃摊位存在时手工删除缓存，否则会丢失占用证据并增加重叠概率。需要清理时应先停止自动调度、确认摊位已释放，再删除两个文件后启动 Robot。

## 12. 摊位到期和恢复

摆摊持续时间到期、Actor 释放或失败清理时：

1. 先让角色进入离线写边界并清理缓存。
2. 删除普通摊位记录和临时私人商店权限。
3. 把 `Dummylist.function_type` 恢复为 0。
4. 从有效地图重新选择普通位置，并同步角色村庄。
5. NoCache 后把角色恢复为普通在线机器人。
6. 释放点位 claim 和 occupancy。

这样不会让游戏最终快照重新写回旧背包或旧私人商店权限，也不会长期保留可见摊位外观。

## 13. 移动、喊话、跟随和组队

### 13.1 移动

移动在 Actor 内串行执行，按配置速度、移动类型、步数和间隔生成包。位置写入通过批处理器合并，降低高在线数下的 SQL 写放大。

### 13.2 喊话

- 本地喊话走 DNF 角色协议。
- 世界喊话使用世界喇叭并走 Monitor 能力。
- 文本模板按文件时间戳缓存，去重并限制为安全字节长度。
- 自动调度分别记录本地和世界喊话成功率。

### 13.3 跟随和组队

固定跟随账号可决定出生村庄和活动范围。组队协议包含 Relay、UDP、TQOS、路由降级/恢复、队伍状态、地下城跟随和技能状态匹配。

Web 的 `Party` 功能是针对已验证 df_game_r 版本的内存兼容补丁。补丁应用前校验目标签名，暂停进程、写入并回读校验；签名不匹配时拒绝修改。Supervisor 会在游戏重启后恢复期望状态。

## 14. 拍卖行与金币寄售

Market App 管理两类市场：

- Auction：普通拍卖行物品。
- Cera/Point：金币寄售。

工作链路：

1. 从 PVF `iteminfo` 和配置构建候选队列。
2. 读取系统卖家现有库存，按物品种类和目标数量计算差额。
3. 对普通材料按 stack limit、单价和总价安全边界拆分。
4. 对装备生成强化、附加信息、价格和特殊实例。
5. 对宠物先创建 `creature_items` 实例，再把实例 ID 作为 add_info 上架。
6. 通过 Auction/Point 原生注册包执行，并解析明确成功/失败回应。
7. 从数据库回读落地结果，更新策略健康度、完成度、停滞轮次和拒绝队列。

`max_actions=0` 只取消正常候选的单轮上限；服务端已拒绝的候选仍按 10 轮冷却和低权重预算重试，避免全量模式反复发送同一批必拒包。
8. 回收通过原生竞拍包完成，不直接删除用户正常订单。

### 14.1 耐久字段

只有以下耐久装备类型设置 `HasEndurance=true` 并写协议偏移 `0x3a..0x3c`：武器、上衣、肩、裤、鞋、腰带。

非耐久度类型保持 `HasEndurance=false`，协议字段不写。内存结构中的数值零只是 Go 零值，不代表向拍卖协议显式写入耐久 0。

### 14.2 价格规则

- 可堆叠物品使用 `start_price=-1`，支持按件购买路径。
- 装备起拍价小于一口价。
- 单价、数量和总价均限制在协议整数范围内。
- 系统卖家按配置范围轮转，避免单账号记录过度集中。

### 14.3 服务兼容和补丁

- Auction/Point 服务目录从 `DfGameR` 推导的服务根目录定位。
- 启动前检查二进制、端口、`iteminfo.dat` 和月度历史表。
- Auction 搜索保护补丁修改文件前保留备份，并使用标记保证可重复执行。
- Auction 内存补丁仅在 PID、地址和期望字节匹配时写入，并逐项回读。
- `marketClearSystemStock` 只清理系统卖家范围及对应宠物实例。

## 15. Web 管理端

Web 使用密码登录和带过期时间的内存会话。主要能力：

- Robot 状态、在线、移动、喊话、摆摊、Logout、清理和自动开关。
- 目标在线数和可见配置编辑。
- `Max` 修改所有 df_game_r cfg 中的 `max_user_num`。
- `Ports` 修改服务端口，重启后生效。
- `Script` 执行受限的 `/root/run`、`/root/stop` 服务控制。
- `Market` 查看服务和策略，直接调整自动周期、市场范围、装备等级/稀有度过滤、装备生成、价格公式、补货限制与虚拟回收概率；自动、补货和回收分别保存动作数及并发限制。窗口不编辑单品价格 JSON，仍提供补货、回收、iteminfo 和补丁操作。
- `Party` 管理组队兼容补丁及技能开关。
- 右上角 `Compat` 管理 mailbox bad-node guard。
- `Diag` 汇总文件、进程、端口、数据库、PVF、密钥、市场、补丁和日志检查。

Web 只作为 Robot TCP API 的受控代理，不维护第二套机器人业务状态。

## 16. 70 布局的 Compat 补丁

70 开头布局需要在 Web 右上角 `Compat` 中启用 mailbox bad-node guard。该补丁：

1. 跳过无效物品清理期间的邮箱附件扫描。
2. 对损坏的 mailbox stream list 零头指针按 empty 处理，避免访问 `0x4`。
3. 应用前校验两处上下文签名。
4. 暂停 df_game_r 后写内存并逐处回读；部分应用或未知字节会回滚并报错。
5. 期望状态保存到 `compat.json`，Robot/Web 重启后 Supervisor 自动重放。

此补丁只适用于签名匹配的 df_game_r。其他布局显示 unsupported 时不得强行写入。

## 17. 诊断、缓存和日志

主要运行文件：

- `log_robot`：Robot 主日志，按大小轮转。
- `robot_stdout.log`：主进程和 Web 子进程 stdout，经 bounded sink 轮转。
- `market_log.jsonl`：Market 结构化事件日志。
- `mail_notify_cursor.json`：Robot 内置邮件通知轮询的 letter/postal 游标和待发送角色。
- `pvf_manifest.json`：PVF 和运行文件自检。
- `store_points_cache.json`：点位历史。
- `store_points_active.json`：活跃点位租约。
- `party_compat.json`：Party 补丁期望状态。
- `compat.json`：Mailbox guard 期望状态。

诊断接口检查：

- Robot 构建信息和 Git VCS 元数据。
- 预期端口与实际监听进程。
- Game UDP NoCache 端口。
- RSA 公私钥与游戏目录文件一致性。
- PVF 导出新鲜度。
- 数据库连通性、连接池和核心表。
- Auction/Point 服务、策略、iteminfo、文件补丁和内存补丁。
- Auto 中的 `Mail / Refresh` 默认开启。Auto 调度轮询时同步检查 `taiwan_cain_2nd.letter` 和 `postal` 的新增系统/GM 邮件（`send_charac_no=0`），并通过 Monitor 的原生 `0x0514` 新邮件通知包提醒在线角色；首次运行只建立当前游标，不重放历史邮件。关闭 Auto 或取消该勾选后不再轮询，不依赖 Market 或 DP2。
- 近期 panic、fatal、超时、连接和市场错误关键字。
- 日志是否超过配置上限。

## 18. 核心数据库边界

机器人身份和调度：

- `d_starsky.robot_registry`
- `d_starsky.Dummylist`
- `d_starsky.v4_ai_user`

普通摆摊：

- `d_starsky.Robot_stall`
- `d_starsky.Robot_stall_config`
- `taiwan_cain_2nd.inventory`
- `taiwan_login.member_premium`
- `taiwan_login.dnf_event_entry`
- `taiwan_prod.prod_buy_user`
- `taiwan_prod.pu_user_list`

角色：

- `d_taiwan.accounts` 及 member/login/security 关联表。
- `taiwan_cain.charac_info`
- `taiwan_cain.charac_stat`
- `taiwan_cain.charac_view`
- `taiwan_cain.charac_expert_job`
- `taiwan_cain_2nd.inventory`
- `taiwan_cain_2nd.user_items`
- `taiwan_cain_2nd.skill`

市场：

- `taiwan_cain_auction_gold.auction_main`
- `taiwan_cain_auction_cera.auction_main`
- 对应月度 history/history_buyer 表。
- 特殊物品所需 `creature_items` 等实例表。

清理机器人先从 `robot_registry` 识别候选，并检查真实账号、Dummylist 和跨库引用。默认只 dry-run；强制清理时先释放在线 UID，再批量删除已确认属于机器人的账号和角色关联数据。

## 19. 性能实现

当前性能收敛重点：

- 地图区域使用整数键，避免热路径字符串拼接。
- 点位打包和 occupancy 使用网格索引，只扫描相邻单元。
- 位置更新使用批写，降低高在线下 SQL 次数。
- PVF、名字、喊话和表结构使用受控缓存，文件变化或容量边界时失效。
- 摊位商品池在启动时构建，开摊时只做 UID 稳定抽样和固定槽位复制。
- 普通摊 7 条记录在单事务中替换，减少部分状态和重复 SQL。
- Actor 串行化单 UID 动作，跨 UID 通过有界并发扩展。
- TCP 服务有连接数、读写超时和消息边界限制。
- 数据库连接池设置最大连接、空闲连接、空闲寿命和总寿命。
- 日志不再维护无消费者的五级运行状态，只保留必要的写入、flush、轮转和关闭同步。

## 20. 已知边界

- 当前验证过的活跃摊位总目标上限为 108；继续提高需要先扩充无冲突点位证据，而不是只增加并发。
- 7 件普通摊依赖 PVF 中至少存在 3 个合格材料和 4 个合格封装装备；池不足时按实际数量摆放。
- NoCache 是明确的缓存失效请求，但仍要求先确认 Runtime 已关闭，不能替代离线写库边界。
- Party 和 Mailbox 内存补丁只支持签名匹配的 df_game_r。
- 三个测试 VM 使用同一 IP，任何时刻只能启动一个，否则无法判断当前服务布局和测试结果归属。

## 21. 三布局兼容验收

每种布局必须单独开机并完成：

1. 确认 `DfGameR`、服务根目录、进程和端口与当前 VM 对应。
2. 部署前记录 Git commit，备份 `/root/robot`，上传 `/root/robot.new` 后原子替换。
3. 启动 Robot，检查 `8111`、`8112`、`10011`、`30303`、`7200`、`30603`、`30803`。
4. 检查 Web、数据库、RSA、PVF 和 Market 诊断。
5. 创建少量角色，检查等级、职业、装备、至少配置数量的装扮、出生村庄和坐标。
6. 执行登录、移动、本地/世界喊话、Logout+NoCache。
7. 检查普通摊商品数、背包对应关系、分解摊、点位不重叠和到期恢复。
8. 检查拍卖行普通物品、装备耐久语义、特殊物品、金币寄售、回收和 iteminfo。
9. 检查 Party 状态；70 布局额外启用并验证右上角 `Compat` mailbox guard。
10. 检查日志无 panic/fatal、进程无异常重启、数据库连接和文件描述符正常。

布局按顺序逐个验证，完成一个后关机，再启动下一个。

## 22. 最终一次完整压测

最终压测在 70 VM 执行一次完整、按依赖排序的确定性轮次：

```sh
python2.7 /root/vm_random_stability.py 1
```

脚本会先读取、写入并回读校验 `max_user_num=max(1000, target_max)`；默认 target_max 为 600，因此符合“先 Max=1000，再跑目标 600”的要求。只有值实际变化且 `df_game_r` 正在运行时才重启核心服务使限制生效。

完整轮只有两个顶层矩阵：

1. `integrated_load_data_matrix`：负载预热、高负载共享观察、Market 正常流程、Market 服务/数据源故障、数据库故障和手动机器人流程。
2. `restart_recovery_matrix`：配置目录回退，以及 Web、Monitor、Core、Bridge、兼容补丁和密钥的合并恢复。

负载预热按 `20→300→600` 单向收敛，避免在创建过程中用 `600→80→600` 人为触发熔断；降载验证合并到最终恢复的 `600→20`。自然摊位证据在任何手动摆摊动作前独立采集，满足稳定样本、摊位类型、数量和 7 件比例后即可提前结束；NoCache 与 Party 使用跨日志轮转的游标读取，不依赖尾部文本猜测增量。Market 的 iteminfo 和数据源故障分别批量执行、各只做一次健康恢复，坏 Market 配置并入配置目录阶段。Market 自动补货默认限制为 512 条，避免 10000 条任务阻塞后续故障注入。周期日志归档不会在主循环启动时立即执行，游戏连接异常由增量游标采样，不再每次扫描整天日志。默认单轮关闭随机目标、随机清理和随机用户操作插入，多轮或显式时限模式仍保留这些扰动。

服务恢复先固定一个可用动作 UID，依次验证 Web 独立故障和 Monitor 独立故障，再调用 `autoStop` 排空调度、停止 Market 并执行 `/root/stop`。只有 Game `10011`、Monitor `30303`、Bridge `7000` 全部关闭后才替换密钥；核心通过 `setsid sh -c 'cd /root && ./run' </dev/null` 脱离 SSH PTY 启动，并要求三个端口连续 3 个样本稳定。该顺序在 600 目标实测中把核心关闭前排空从约 98 秒降到约 8 秒，数据仅作为本次实现的对照，不作为固定时限承诺。

数据库连接故障不重启 MySQL，也不影响 Bridge 或其他服务连接。脚本从 Robot 主进程的 `/proc/<pid>/fd` 获取 socket inode，经 `/proc/net/tcp*` 反查本地客户端端口，再匹配 `information_schema.PROCESSLIST.HOST`，只对命中的连接执行 `KILL CONNECTION`。恢复必须同时满足旧连接全部消失、出现新连接、连接池 Ping 与 `SELECT 1` 成功、Robot API 和 Market 保持健康。

每个阶段结束及最终恢复后都会扫描 `/home/neople/*/core.*` 和 `/home/dxf/*/core.*`。测试开始前已有且未变化的文件忽略；新增路径或同路径被新 inode/大小/时间替换都视为新 core，写入 Coverage、Failure 和 `summary.json.new_core_dumps`。

通过条件：

- `summary.json.failure_count == 0`
- `failures.json` 为空数组。
- `report.md` 所有事件为 OK 且最终环境恢复。
- `report.md` 的 Coverage 明确区分“已观察”和“本轮无自然活动”，不把跳过项当作已覆盖。
- 目标 600 的稳定采样中在线达到至少 95% 目标。
- 目标 600 至少取得 3 个稳定样本，摊位峰值不低于 `max(10, target/30)`，普通摊和分解摊均有活跃样本。
- 普通摊显示物品数只在 1..7，0 件和越界为 0，稳定样本中的 7 件比例不低于 90%。
- NoCache 在完整负载阶段内必须实际触发，且精确日志增量中的失败为 0；摊位恢复失败为 0。
- Market 服务 ready，Auction/Cera 策略无持续停滞或失败轮次。
- Game、Monitor、Bridge 在恢复阶段连续 3 个样本稳定，且 `summary.json.new_core_dumps` 为空。
- 无 panic、fatal、too many open files、持续连接超时或无法恢复的 Party 路由错误。

## 23. 发布和 Git 历史

发布顺序固定为：

1. 代码、文档和本地质量门完成，过程中分步本地提交，暂不推送。
2. 在当前 70 VM 执行唯一一次完整两矩阵压测并通过。
3. 生成本地 Git bundle，保留首版检查点和压扁前完整历史 HEAD。
4. 确认远端 `main` 未变化，将最终工作树压成一个清晰根提交。
5. 使用锁定旧远端提交的 `--force-with-lease` 强推 `main`，不使用无保护的 `--force`。
6. 核对远端树、构建产物、最终压测报告和桌面实现文档。

三种同 IP 布局的完整兼容验收仍按第 21 节逐台执行，但不与本轮唯一一次 70 VM 压测交叉，避免代码收敛后重复制造不可比较的中间结果。

压测未通过前不得压扁历史；压测或兼容验收发现问题时，先在本地形成可追溯提交并重新执行受影响的检查，最终通过后再进入压扁和推送阶段。
