# VM 连接信息

本文给 AI/维护者使用。普通使用者只看 `doc/使用说明.md`。

## 连接信息

- VM IP：`192.168.200.131`
- SSH：`root / 123456`
- Web：`http://192.168.200.131:8112`
- Web 密码：`twadmin`
- robot TCP API：`8111`
- 游戏端口：`10011`
- robot 程序：`/root/robot`
- 配置目录：`/root/config`
- 主配置：`/root/config/config.ini`
- 行为配置：`/root/config/robot_config.ini`
- 主业务日志：`/root/config/log_robot`
- 启动输出与 Web 诊断日志：`/root/robot_stdout.log`
- 启动全服：`/root/run`
- 停止全服：`/root/stop`
- 游戏目录：`/home/neople/game`
- df_game_r：`/home/neople/game/df_game_r`

## VMware

- VMX：`D:\cache\game\DNF_85_CUICAN_20260407_璀璨85完结版\DNFServer\DNFServer 7.3 x64.vmx`
- vmrun：`C:\Program Files (x86)\VMware\VMware Workstation\vmrun.exe`
- 最后快照名：`快照 2`

恢复最后快照后启动：

```text
vmrun -T ws revertToSnapshot "<VMX>" "快照 2"
vmrun -T ws start "<VMX>" nogui
```

AI 执行约束：

- 连接 VM、上传文件、执行远端命令时，优先使用 Python SSH 工具 `paramiko` 或者调用 `vmrun`。
- 禁止用 PowerShell 直接执行 SSH、SCP、vmrun 等 VM 操作，避免编码、转义和交互行为不一致。
- VM 网络偶发波动时可以等待后重试，不要立刻判断部署失败。
- VM 响应慢通常是 VMware CPU 占用导致的卡顿，先等待或降低并发操作，不要误判为网络断开。
- 需要恢复/启动虚拟机时，只通过 `vmrun` 执行，不要手动改 VM 文件。

## 本地构建

在仓库根目录执行：

```text
cd robot-go
go test ./...
go vet ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../bin/robot ./cmd/robot
```

Windows PowerShell 示例：

```powershell
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go build -trimpath -ldflags='-s -w' -o ..\bin\robot .\cmd\robot
```

## 部署

1. 记录当前 git commit。
2. 上传 `bin/robot` 到 `/root/robot.new`。
3. `chmod +x /root/robot.new`。
4. 备份旧 `/root/robot`。
5. 停止旧 `/root/robot` 进程。
6. 替换 `/root/robot`。
7. `nohup /root/robot >/root/robot_stdout.log 2>&1 &`。
8. 执行 `/root/run` 启动游戏服务。
9. 等 `10011` 连续稳定后再启动自动调度。

## 日志定位

- 机器人业务、调度、actor、摆摊、登录：查 `/root/config/log_robot`。
- Web 子进程启动、退出、panic、慢请求、auth rejected：查 `/root/robot_stdout.log`。

常用命令：

```text
grep -a -E 'WebAdmin|web admin exited|web admin listening|panic|request pid|auth rejected' /root/robot_stdout.log
grep -a -E 'panic|fatal|msg_queue_full|broken_lease|broken_cleanup|SchedulerPolicy|RobotMetrics' /root/config/log_robot
```

Web session 安全要求：

- token 只保存在 web 子进程内存。
- 支持多个浏览器/页面各自持有随机 token。
- 不允许把 token/session 固化到本地文件。
- 如果 web 子进程被打崩，刷新回登录页是安全预期；必须从 `/root/robot_stdout.log` 追查退出原因。

## 打包

git提交，交付 zip 放到桌面，命名格式：

```text
dofrobot-main_YYYYMMDD-HHMMSS_最后更新的功能xxx.zip
```

包内包含：

- 源码
- `.git` 目录，用于下次继续修复、查看历史、提交和回滚
- `doc`
- `bin/robot`
- 默认资源

包内不包含：

- 临时诊断文件
- 旧基线产物
- 旧压测摘要
- 桌面旧 zip

## 代码检查

提交前执行：

```text
cd robot-go
gofmt -w ./cmd/robot ./internal ./pkg
go test ./...
go vet ./...
```

默认行为配置使用 `[spawn]` 管理出生点和活动范围，不再使用旧的调试命名。
