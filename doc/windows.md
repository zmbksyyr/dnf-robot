# Windows 宿主

Robot 保持独立项目。Linux 使用原有 `/proc`、信号及启动脚本；Windows 使用同一业务代码和本项目的平台实现。宿主差异由配置目录内 `conf/host-profile.json` 描述，不依赖外部 GUI 的进程名称或界面控件。

## 接口和生命周期

`--config-dir` 指定 Robot 自己的配置、模板、PVF 导出、密钥、状态和日志目录。Windows 可选 `--stop-event`、`--ready-event` 分别接收正常停止请求、发出首次初始化完成通知。外部控制器只需要准备 game 目录中的配对密钥并提供宿主 profile；Robot 自行同步密钥和初始化业务文件。

profile 为 version 1，包含服务的 executable、image、image_sha256、pid_file、directory、start 参数及 stop_event。Windows 在接受 PID 前核对进程路径、原 ELF 文件 SHA-256 和已映射 ELF 头；不把同名进程当作目标。game、auction、point 为运行契约必需项，monitor、relay 可提供用于诊断中的身份关联。未知目标或旧字节不同明确失败。

Web 的重启操作在 Windows 同一 PID 内关闭并重建 Robot 的运行资源，保留外部控制器的进程和停止事件归属；仅首次启动使用 ready 事件。Linux 保留原有进程重启路径。停止会先取消新的后台工作与未完成登录确认，再等待已经开始的操作退出；错误关闭返回非零状态。

HTTP 子进程关闭监听后，还会等待 `http.Server.Shutdown` 排空活动响应，再返回并退出。此前只等待 `ListenAndServe` 返回，带 42 个摊位热重开时曾使 HTTP 重启请求收到连接重置，尽管 Robot 主 PID 已成功重建。这是公共 HTTP 生命周期修复，Linux 同样等待已有请求结束，不改变 Linux 的重启入口、脚本或业务协议。真实 HTTP 回归在移除该等待时失败，恢复后连续 10 次通过；全仓测试、Web race 及 Linux amd64 交叉编译通过，记录为 Native 仓库外 `robot-http-regression.json`、`robot-http-full.log`、`robot-http-race.log`。

关闭也会中断同账号重登、选角存档、离线清理和分解摊确认等待。离线清理返回取消错误，不能作为数据库写入的安全许可；正常运行和仅停止自动行为时仍保持原有存档边界。这些取消及批量操作改进属于公共修复，Linux 同样使用。

所有 actor 的正常退出共用 16 个并发名额，覆盖停止自动行为、缩容和进程关闭，
对支持返回选角、当前运行且未组队的角色，先沿用 CMD 7 保存并卸载角色，收到原
服务确认后才关闭连接；进程关闭期间仍可完成这个保存，但不授予数据库写入许可。
保存拒绝或超时明确失败，进入既有强制关闭路径。随后执行原有 500 ms 本地关闭
确认，再查询服务端账号状态：每 100 ms 检查一次，
最多等待 5 秒，账号离线后才处理角色缓存并释放名额。查询失败或超时明确记录失败，
不确认退出成功，也不清理缓存。这只控制退出负载，不替代既有存档稳定期和写库屏障。
停止请求立即发给所有 actor，先停止自动动作；连接随后分批退出，避免本地连接已关闭、
服务端仍未清理的角色积累并产生大量失效广播。手动批量退出沿用相同上限。
内部关闭期限为 25 秒，其中最后 2 秒保留既有强制清理；600 人仅确认等待就需约 19 秒，
原 15 秒期限不足。GUI 的 30 秒等待、退出失败报告和原协议保持不变，两端共用此调度。

## 平台行为

| 功能 | Windows 行为 |
| --- | --- |
| 创建、上下线、移动、喊话、普通摊和分解摊 | 共用业务和协议实现；手动批量上线沿用登录速率，所有 actor 正常登出共用 16 个并发名额 |
| 分解摊 | 等待完整登录后，单独启动 20 秒机器确认窗口，避免登录过程提前耗尽确认预算 |
| 市场服务及 iteminfo | 从 profile 启停原服务；重建与同步使用现有备份、冲突检查和恢复逻辑 |
| 市场内存补丁 | 根据宿主内存模型核验地址、旧值、写入和回读；Windows refine-banks-31 保留均价上限 7，搜索上限 31，等级上限 127 |
| 组队和邮箱补丁 | 对已验证 game 的线程暂停、核对并修改内存、回读及恢复线程；不安装驱动或系统服务 |
| 运行脚本 | `/root/run`、`/root/stop` 在页面置灰，API 同样明确拒绝；由宿主控制器启停 |
| 在线上限 | 读取实际 game 配置，允许 1–600；修改后提示重启 game |
| 资源与诊断 | Windows API 统计实际 CPU 时间和工作集；netstat 与 tasklist 结合 profile 显示真实服务身份 |

Windows 的 CPU 百分比与 Linux 相同，以单核算力为 100%；工作集和 Linux RSS 不等于 Go 堆大小。socket 使用可复用的 32 KiB 读缓冲，断开时释放库存、摊位、组队和城镇缓存，保留重连所需令牌及密钥。

普通摊、分解摊的准备和到期收摊共用已有并发名额。名额不足时保持现有摊位，下一轮重试到期清理；清理后的恢复必须等角色实际进城才释放名额。此前收摊不受限制、会话确认早于进城，使准备和清理叠加，600 目标下曾出现 546 运行、51 连接中。这是公共调度修复，Linux 同样适用，未改变保存完成后才允许写库的边界。

拍卖搜索保护 v7 使用已有孔记录覆盖包数据，取消可能对缺失 PVF 物品解引用的 `CDataManager/CItem` 调用；保留无记录时的原包、重入保护及异常记录。Windows 首次初始化会更新已识别脚本段并备份，Linux 保留原有显式安装方式。未知自定义 hook 拒绝自动改写。

## 验证边界

本轮测试机为 Windows 10 IoT Enterprise LTSC 2021 x64，build 19044；Go 1.26.4，Windows amd64 无 CGO 交付。普通全仓测试、全仓竞态检查及批量操作变更后的相关竞态检查通过；Linux amd64 无 CGO 交叉编译通过。原版 ELF、数据库和业务集成使用独立副本，证据保留在同级 `x/dnf-native-output/`。

原版市场启停、六项内存补丁回读、真实角色登录/移动/登出，以及 GUI 管理的正常停止、重开和密钥初始化已有实测。独立实例达到手动 600 在线并保持 60 秒；600 目标混合自动行为连续 180.3 秒至少 550 在线，Robot 工作集 256–366 MiB，game 约 1556 MiB。605 个账号全部确认登出，操作约 19.9 秒。580 目标混合阶段没有持续达到 550+，保留该失败；自动摊位和登录仍有失败，不把短观察写成长时间稳定性保证。

596 在线且 59 个 actor 忙碌时，Web 在同 PID 内约 18.3 秒完成重建，之后实际登录、移动、登出通过；首个同步登录先返回 pending，后续确认另存记录。600 批量上线正在连接时正常停服约 1.42 秒退出 0，game 保持运行，重开密钥可用。只断开 Robot 实际 SQL 连接后的重连/SELECT、CID/UID/范围删除及备份后精确恢复均已实测。证据位于 `load-550/` 的对应私有收据，不将账号或数据库行纳入源码。

独立实际客户端通过默认登录器进入 85 级角色，拍卖搜索返回 3 条“流光星陨刀”订单，重复 10 次仍正常；100 万金币寄售上架、搜索自己的订单、取消成功，SQL 确认测试订单移除。证据为 `client-ready/business-receipt.private.json` 和操作截图；物品交易及邮件由下一段追加测试补齐。

追加实际客户端测试使用 Native 邮箱初始化修复后的宿主，完成购买 2000 个无色小晶块、领取邮件、重新上架、公开关键字搜索、撤回再领取和前轮寄售退款领取。正常登出后 SQL 确认背包、金币、邮件和测试订单一致；证据为 `client-ready/trade-fixed-receipt.private.json`。邮箱标量初始化修复属于 Native 宿主，Robot 继续使用原协议和市场业务。

收摊并发修复经全仓普通测试、actor/scheduler race 及 Linux amd64 交叉编译通过，见 Native 仓库外 `robot-store-churn-fixed/checks.json`。同一测试 game 热重开 Robot 后曾在 10 分钟内未达到 550，随后正常停服超过 180 秒；完整根因尚未定位，`load-550/soak-store-churn-fixed.private.json` 和 `coldstart-store-churn.private.json` 保留失败，不能将并发修复或此前短负载成功当成这两项已解决。

最终宿主与收摊修复从全新 game 进程启动后，600 目标混合负载连续 900.0 秒、每 15 秒采样保持 555–599 运行，Robot 工作集 225–334 MiB，game 1448.1–1462.2 MiB，累计到期收摊 172 次。观察段新增移动 20571 次、本地/世界喊话 6137/6183 次、成功摆摊 234 次；最终登录、移动、喊话失败计数为零，仍有 99 次摆摊失败。自动退出约 146.7 秒，整套正常停服约 43.1 秒，无强制结束；证据为 `load-550/soak-store-churn-cold.private.json`、`lifecycle-store-churn-cold.private.json`。以上为 Windows 干净进程路径的实际观察。

最终又在 599 运行且有忙碌 actor 时 Web 热重启，约 15.6 秒重建运行环境；同一个 game PID 重新达到并连续 60.1 秒保持 586–598 运行，随后自动退出约 140.6 秒、整套正常停服约 36.5 秒，无强制结束。`load-550/restart-store-churn-final.private.json` 记录这次通过，旧长生命周期测试进程中的失败和根因边界仍保留。对应 `51264a2` 的业务源码及嵌入资源哈希与交付程序构建收据一致，Native 正式 `DNF-Native-3.0` 已更新，交付记录留在 Native 仓库外。

上述 Windows 阶段当时没有 Linux 实际运行资源对照数据，后续同机对照见下文。实际客户端操作与保护夹具分开记录，不能用交叉编译、端口开放或单测代替业务结果。

追加候选与 Native 的内存查询缓存、进程信号投递修复一起实测：分别在 596／598／595 在线、42／63／75 个活跃摊位且有忙碌 actor 时 Web 重开三次，HTTP 均正常应答；Robot 主 PID 和 game PID 均保持。每次约 15.6 秒重建，每轮恢复后连续 120 秒保持 550+，样本 572–600。Robot 主进程在 550+ 样本中的工作集 185–252 MiB；自动退出约 62.4 秒，整套正常停服约 63.0 秒，无强制结束。收据为 `load-550/http-signal-cache-restarts.private.json`。批量重连／退出时仍有最多约 50.8 秒的游戏处理积压，不能把稳态恢复解释为瞬时恢复；Linux 原生资源及耗时对照仍单列。

## Linux 实际对照（2026-11-18）

在同一台 Windows 10 x64 19044.1288 测试机上，先后运行 WSL2 5.10.16.3/Ubuntu 22.04.5 中的原版 ELF 与 Native Windows 宿主；Windows 开始前已正常停服并终止 Linux 测试发行版。两端使用相同 stock PVF、MySQL 5.7.44 冷库、600 个新建 50–70 级角色及 DP2/Frida。Go 1.26.4 amd64、CGO 关闭；构建收据的 513 项源码和内嵌资源哈希与 `1a17552` 一致。它是同机 Linux 内核对照，不是裸机 Linux 结论，也没有把 WSL 引入 Windows 产品依赖。

Linux 先实际创建 600 个角色，再以同一冷库验证两端登录、移动、600 在线持续 60 秒、批量登出、连续 300 秒 550+ 混合行为，以及 Web 重启后连续 120 秒 550+ 恢复。首段有效混合样本 Linux 590–600，Windows 589–600；两类摊位均有成功记录，也保留失败计数。Linux 首次角色创建与六项市场内存补丁实际回读通过，组队/邮箱状态已实际读取；Windows 角色创建、各补丁及客户端业务的原有证据仍单列。

混合样本的 Robot 全进程树驻留均值为 Linux 240.2 MiB RSS、Windows 291.3 MiB 工作集，包含 Web；Windows 另计 Web 的 conhost。不能将 RSS、工作集、Go 堆和私有提交混为一个数值。600 上线操作约 30.09/50.19 秒，批量登出操作约 20.09/20.13 秒，自动行为排空约 30.24/60.33 秒（Linux/Windows）。game 秒级时钟的最大观测积压为 6.02/52.68 秒；稳态恢复不等于批量切换无延迟。

最终整套正常停服为 26.52/41.83 秒；Linux 无新增 core dump，Windows 无未处理异常、额外兼容缺口或残留测试进程。Windows 的两个 inotify ENOSYS 属于 Native 已有 Frida 轮询回退边界，不是 Robot 功能通过就能推导的通用 syscall 兼容承诺。原版部分辅助服务正常信号退出返回 1，保留原值。

当前用户 PVF 在本轮固定原 ELF/插件/Linux 环境中仍于装备布局的 `snprintf` 错误路径崩溃，故性能基线另选 stock PVF 和匹配角色，未将不匹配的旧订单/假人数据当作通过。测试副本清理前完成冷备份、逐文件恢复和 SQL 验证，原用户数据库不变。proc/dev/run-shm、三个漏拷的 DP2 core 模块及精简环境缺少 nohup 均属于本轮测试环境修正；Linux 原有重启脚本及业务代码未因此改动，首次失败记录保留。

完整证据在 Native 仓库外 `x/dnf-native-output/linux-comparison/`：`comparison-summary.json`、`Linux与Windows同机验收对照.md`、两端 benchmark 私有收据及冷数据哈希。此次仅新增对照记录，未替换已交付二进制。

## 中文资源名称修复（2026-11-18）

城镇名称曾显示 `name_18130` 等字典键：脚本解析没有处理 type 9 字典编号与后续
type 10 名称键。现在按 n_string.lst 定位字典并解析引用，包含合法的字典编号 0；
缺失引用明确显示 ErrorString，保留其后标签，不伪造名称。
stringtable 按实际文本条目跨表采样，整表统一识别 UTF-8、GBK 或 Big5，避免对每个
短名称独立猜测；修改 PVF 的字符串读取路径复用同一解析。码页转换每次使用独立
Decoder，导出版本 6 升至 7，旧目录自动重建。

Windows 的同一测试实例正常停止 Robot 单模块后换候选、重新启动，已自动重建
版本 7；真实角色登录后 API 城镇为“西海岸”。独立 Linux 原 ELF 副本也实际启动、
登录并返回“阿法利亞臨時營地”。只读解析两份实际 PVF，stock 的物品 1 为“復活幣”、
城镇为“阿法利亞臨時營地”，用户客户端包对应“复活币”与“阿法利亚营地”。
两份包保留各自语言，不以简繁不同判定乱码。公共解析修复同时用于 Windows/Linux，
未修改 Linux 的进程管理、启动脚本、数据包或游戏内存补丁。

新增字典引用、零编号、缺失引用、三种码页的测试及可选真实 PVF 集成用例。
`go test ./...`、Windows/Linux amd64 无 CGO 构建通过；实际证据位于仓库外
`../x/dnf-gm-output/evidence/encoding/`。不将本轮名称修复解释为此前全部负载及
客户端业务重新跑过。

## 摆摊碰撞与后台子进程（2026-11-18）

用户指定的 rednf 仅用于只读理解位置逻辑，查阅
`CVillageObjectMgr/register_object.inc`、`CVillageObjectMgr.h`、
`CPrivateStoreMgr/CPrivateStorePos_1.inc` 和
`ProfessionDisjointer/CDisjointMachine.inc`。随后独立核对 SHA-256 为
`da417c5e65918f655afcddb6edfdaf2fa1e77b2f208e5aa397f021fa23b79979`
的原 game：普通摊构造函数 `0x085cb7fc`、分解摊构造函数 `0x085d1ae8`、
矩形判断 `0x085cb492` 确认两类对象均为 160×100，接触边界也判碰撞。
来源文件哈希与原 ELF 反汇编保留在仓库外
`x/dnf-gm-output/evidence/stall-position-source-study.json` 及
`stall-*-original-disassembly.log`；未引入参考代码、脚本、补丁或产物。

Robot 原有网格横向步长 120，占位检查也只排斥到 119，会允许两个碰撞框重叠。
现在保留网格和既有纵向间隔，横向占位范围修正为包含 160；正常网格因此隔列选点。
普通摊首次登录仍使用准备坐标，后续换点复用会话时，在同区域也发送原有移动请求。
创建失败日志增加实际区域、坐标和服务端错误码；成功仍要求原服务的创建、展示确认。
这两项为公共调度修正，同时用于 Windows 和 Linux，不修改原 ELF。

Linux 实测进一步复现重试读取旧错误：底层已同步清除上次拒绝结果，调度层仍可能
命中 2 秒缓存，因而在新回包前判失败。成功启动新普通摊尝试后现立即使该缓存失效，
与分解摊已有处理一致；不延长确认期限或伪造成功。回归用原生产源码 overlay
复现、当前源码通过，证据为 `stall-cache-regression-before-after.json`。

Windows 的 Web 子进程和 netstat/tasklist 诊断命令统一以 DETACHED_PROCESS 启动，
保留标准输入输出、命名停止事件和原有监督逻辑。Linux 使用原来的 exec.Command，
没有新增配置、启动脚本或外部 GUI 依赖。Windows 实际诊断刷新与重开后均未发现
控制台所属 conhost 或可见黑窗；重开后原 game 未重启，实际登录、移动、登出通过。

最终三项公共摆摊修复完成后，Windows Native 与 Linux 原 ELF 独立副本均完成
600 目标、120 秒稳态混合测试：在线范围分别为 593–600、597–600；排空后累计
摆摊成功／失败分别为 84／3、79／3，移动为 3468／0、3340／0。Linux 日志实际
出现普通摊与分解摊换点后成功。两端正常排空、整套停服；Windows 最终版本再次
连续刷新三次真实诊断，没有所属 conhost 或可见黑窗。全仓测试及双平台构建通过。

Windows 自动行为排空约 50.05 秒，Linux 约 30.03 秒；game 秒级时钟最大积压
分别为 35.86、6.01 秒。剩余位置拒绝与 Windows 高峰积压继续处理，不能据这次
随机地图与动态策略负载推导所有位置零失败，或宣称已达到 Linux 原生性能。
Linux 为授权的同机 WSL 独立对照，并非产品依赖或裸机基准。最终收据为 Native
仓库外 `x/dnf-gm-output/evidence/stall-cache-final-summary.json`、两端原始负载 JSON
及 `stall-cache-windows-console.json`；首轮旧错误缓存的失败日志亦保留。

## 自动分解摊的村庄限制（2026-11-18）

Native 联调日志仍出现村庄 7 的分解摊请求及 `0xbe` 拒绝。只读查阅用户提供的
rednf `ExpertJobLink4/OnCreateDisjointStore.inc` 后，独立核对固定原 game
（SHA-256 `da417c5e65918f655afcddb6edfdaf2fa1e77b2f208e5aa397f021fa23b79979`）：
`CDisjointer::OnCreateDisjointStore` 在 `0x085d2d6d` 比较村庄 7，随后返回
`0xbe`，尚未进入坐标检查。该规则不是位置重试能够修复的错误。

公共选点增加 `DisjointAreaAllowed`，只让自动分解摊排除此村庄，并沿用原公会
目的地限制。普通移动与普通摆摊继续使用 `GenericAreaAllowed`；不修改原 ELF、
数据库或配置，不增加宿主差异开关。Windows 的 shared/scheduler/store 测试及
Linux 的 shared/scheduler 测试通过。该公共修复同时用于两个宿主，不代表本轮
再次完成 Linux 全部游戏业务回归。

来源文件哈希、原 ELF 反汇编与独立实现差异保存在 Native 仓库外
`../x/dnf-gm-output/evidence/disjoint-village7-source-study.json`、
`disjoint-village7-original-disassembly.log`，Linux 测试收据为
`sysinfo-robot-linux-policy.json`；未引入参考代码或编译产物。

最终 Windows 独立副本与 Native 查询优化候选一同运行 600 目标、120 秒混合
负载，稳态 595–600 在线，摆摊 81 次成功/0 次失败、移动 3442/0。期间 44 条
分解摊尝试/成功日志没有村庄 7，实际运行的 Robot 哈希与新构建一致；自动行为
正常排空，game 时钟恢复。混合负载包含随机地图，不据此宣称全部位置零失败。
收据为 `sysinfo-final-summary.json` 及 `tls-call-load-sysinfo-robot.private.json`。

## 批量退出的服务端确认（2026-11-19）

沿用固定原 ELF、Windows 10 x64 19044 和授权的同机 Linux 5.10.16.3 WSL2
独立对照副本。只限制本地关闭的 16 并发候选，在 Windows 600 人混合负载停止时
仍有 23.52 秒时钟积压；本地连接先消失、游戏用户后清理，退出期间约 7 万条
发送错误放大了广播和日志负载。该候选没有发布。

新增上文的账号退出确认后，两端各运行 600 目标、120 秒混合负载。
Windows 稳态 595–600 在线，移动 3430/0、摆摊 77/0，最大退出积压 1.09 秒，
发送错误 4810 条；Linux 为 596–600、移动 3389/0、摆摊 69/0，最大退出积压
1.05 秒。静止 600 人正常停止 Robot，Windows 19.53 秒、Linux 19.24 秒退出，
最大积压分别为 1.19、1.10 秒，原 game 未重启。随机负载及 WSL 不是裸机性能基准，
不能据此声称所有地图零失败或整体达到原生性能。

Linux 混合负载还发现原服务登录表残留 5 个在线标记、游戏端已无当前角色。
连接数和 SQL 状态仍需分别核对；本次没有强行清改登录状态。后续 600 人正常
上线/退出后独立 SQL 连续 15 秒全离线。GM 单独修复只读 inspect 的无当前角色
结果，跳过这些泡点收件人；并非放宽实际写入或用连接关闭代替存档完成。

全仓 Go 测试、Linux 的退出确认及关闭测试、两端 GM 六并发增量/泡点/存档和
离线守卫回归通过，GM 测试值恢复。Native 仓库外证据为
`../x/dnf-gm-output/evidence/server-logout-*`、`robot-server-logout-all-tests.log`、
两端原始负载、`robot-shutdown-server-logout-static-{windows,linux}.private.json`。

继续测试自动摆摊运行中直接关闭进程时，仅账号确认的 Windows 候选仍积压
17.67 秒、退出码 1，保留失败现场。最终增加上文的 CMD 7 保存后关闭连接，
两端重新执行 600 目标/120 秒混合负载并直接正常停止 Robot：Windows 23.56 秒、
Linux 22.89 秒退出，最大时钟积压分别 1.01、1.05 秒，game 均保持运行。
Windows 在既有期限末尾清理最后 1 个连接后退出码为 0，不称为全部优雅退出。
Windows 核对账号全离线，Linux 独立 SQL 连续 15 秒确认全离线。
全仓 Go 测试及 Linux 退出/关闭测试通过；记录为
`graceful-save-active-windows.private.json`、
`robot-shutdown-graceful-save-active-linux.private.json`、
`robot-graceful-save-all-tests.log` 和 `graceful-save-linux-tests.log`。

## 摆摊区域权限与明确拒绝（2026-11-19）

只读参考 rednf 的区域装载、交易检查与普通摊创建逻辑后，独立核对固定原 ELF：
`0x08e2b451` 为 `[use private store]`，区域权限关闭时 `0x086c4614` 返回
`0x3e`；普通摊在 `0x085c54b1` 检查村庄 7。参考文件哈希、原 ELF 反汇编和
独立实现差异记录于 Native 仓库外 `store-permission-source-study.json`。

Robot 的 PVF 导出现在读取区域交易开关，关闭时同时排除普通摊选点与探测，
保留普通移动；导出版本升为 8，旧目录自动重建。原分解摊村庄策略改名为公共
`StoreAreaAllowed`，普通自动摊同样排除村庄 7。没有修改原 ELF 或复制参考实现。
手动摆摊遇到明确的创建／展示拒绝时结束等待，并返回实际错误码。

Windows 10 x64 19044 Native 与授权的 Linux 5.10.16.3 WSL2 原版 ELF 副本
分开运行满级测试角色：各 8 个相同坐标，村庄 7、根特皇宫均返回 `0x3e`，其余
6 点展示确认成功。包括过去曾被拒绝的根特、悬空海港、素喃坐标，不能因一次
`0x3e` 将其永久屏蔽。该定点对照不覆盖所有地图或会话状态，也不是新一轮
600 人负载测试。Go 全仓测试、双平台构建与旧生产代码 overlay 回归通过。

原始记录为 `store-permission-{windows,linux}-actual-final.private.json`。
Windows 定点业务完成后，辅助清理的重复批量登出与停服发生重叠，原失败保留；
随后停下 game，按快照恢复并逐表复核，记录为
`store-permission-windows-cleanup-recovery.json`。Linux 收尾自动恢复并复核快照。
