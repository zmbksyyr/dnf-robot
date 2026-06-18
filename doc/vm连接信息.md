# VM 连接信息

本文给 AI/维护者使用。普通用户只看 `doc/使用说明.md`。

## 连接信息

- VM IP：`192.168.200.131`
- SSH：`root / 123456`
- Web：`http://192.168.200.131:8112`
- robot TCP API：`8111`
- 游戏端口：`10011`
- robot 程序：`/root/robot`
- 配置目录：`/root/config`
- 主配置：`/root/config/config.ini`
- 行为配置：`/root/config/robot_config.ini`
- 日志：`/root/config/log_robot`
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

实际执行时优先用 Python `subprocess.run([...])`，避免 shell 编码和转义问题。

## 本地构建

在仓库根目录执行：

```text
cd robot-go
go test ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../bin/robot-linux ./cmd/robot
```

Windows 上同样优先用 Python 设置环境变量并调用 Go。

## 部署

1. 恢复 VM 最后快照。
2. 等 SSH 可连接。
3. 上传 `bin/robot-linux` 到 `/root/robot.new`。
4. `chmod +x /root/robot.new`。
5. 停止旧 `/root/robot` 进程。
6. 替换 `/root/robot`。
7. 启动 `/root/robot`。
8. 执行 `/root/run` 启动游戏服务。
9. 等 `10011` 连续稳定后再启动自动调度。

## 打包

交付 zip 放到桌面，命名：

```text
dofrobot-main_YYYYMMDD-HHMMSS_收敛调度_清理文档_重新编译.zip
```

包内包含源码、`doc`、`bin/robot-linux`、`bin/libantisvrinline.so` 和默认资源；不包含 `.git`、临时脚本、旧基线产物、旧压测摘要。

## 代码检查

提交前执行：

```text
cd robot-go
gofmt -w ./cmd/robot ./internal ./pkg
go test ./...
go vet ./...
```

默认行为配置使用 `[spawn]` 管理出生点和活动范围，不再使用旧的调试命名。
