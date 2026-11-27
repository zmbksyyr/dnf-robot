# Windows 宿主接口

Robot 是独立 amd64 EXE，关闭 CGO，与 Linux 共用业务和协议。Windows 宿主提供服务身份和生命周期控制，Robot 管理自己的配置、资源、数据库连接与调度。

## 运行契约

- `--config-dir`：实例独立目录，包含 `conf/templates/keys/pvf/state/logs/tmp`。
- `--ready-event` / `--stop-event`：首次初始化完成通知、正常停止请求。
- `conf/host-profile.json` version 1：服务的 executable、原 ELF image / SHA-256、PID 文件、工作目录、启动参数和停止事件。
- game、auction、point 为必需服务；PID 关联核对实际进程路径和原 ELF 身份。
- game 目录提供配对密钥；Robot 同步到自己的 keys 目录并初始化资源。

Native 管理模块生命周期。Robot 开启时由 Robot 负责市场维护，避免与 Native 的拍卖维护重复执行。Windows 明确禁用 `/root/run`、`/root/stop` 操作；Linux 保留原 `/proc`、信号及脚本入口。

## 生命周期和性能

- Web 重开在 Windows 同一主 PID 内重建运行资源，等待 HTTP 活动响应排空。后台子进程使用分离控制台方式。
- 自动账号正常退出共享 16 个并发名额，覆盖保存、服务端确认和离线观察；未确认离线的账号不能进入离线写库流程。
- socket 复用 32 KiB 读缓冲，退出时释放会话库存、摊位和队伍缓存。
- 队伍包遍历复用正文切片，动作包单次分配，职业身份按角色缓存；关闭抓包时跳过诊断字符串格式化。
- Windows 工作集、Linux RSS 和 Go 堆分别记录；历史负载数字不能代替当前版本实测。

## 已知协议与数据语义

- 字符表按整表选择 UTF-8 / GBK / Big5；字典编号 0 有效，简繁体由资源自身决定。
- 普通 CTEA / XTEA 与 561 Anti 密码分派分别实现；保留原向量和 CRC 校验。
- 上线从账号实际角色列表解析目标槽位；外部改名后刷新真实名称。
- 登录拒绝保留服务端原因，背包异常不由产品自动删物品绕过。
- 普通摊和分解摊按原游戏的 160×100 碰撞范围选位，同区域换点也发送移动。

地下城状态包实现和实际验收见 [战斗说明](combat.md)。
