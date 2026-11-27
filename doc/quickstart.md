# 构建与启动

在仓库根目录执行，需要 Go 1.22 或更新版本。主程序与部署启动器分别构建。

## Windows

```powershell
go build -o robot.exe ./cmd/robot
.\robot.exe --config-dir D:\robot-config
```

通常由 DNF-Native 管理 Robot 生命周期，传入实例独立的配置目录及宿主 profile。已有运行实例应继续使用自己的配置目录，不覆盖其他实例。

本机联调的保留路径位于 `D:\cache\dnf\x`：

- 服务端：`live/instance`。
- Native：`review-20261125-native/dnf-native-output/package`。
- 客户端：`acceptance-20261125/support-own-executor1/client`；资源链接依赖同级阶段的 `support-peer-control1/client`。
- 当前 Robot 配置：`acceptance-20261127/skill-host-live1/config`。目录名沿用旧阶段，当前战斗不依赖 skillhost。
- 当前候选和状态包证据：`acceptance-20261127/state-combat-skills1`、`state-combat-live1`、`state-packet-cycle1`。

本机当前 Robot API / Web 为 `18111 / 18112`，游戏为 `10011`；部署默认端口见 [VM 说明](vm.md)。

## Linux

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o robot ./cmd/robot
./robot --config-dir /root/config
```

由 Windows 交叉构建时先设置同名环境变量，再执行 `go build`。部署与正常启动遵循 [VM 说明](vm.md)；原游戏服务端运行要点见 [Linux 说明](linux-server-recipe.md)。

## 检查

```sh
go test ./...
go vet ./...
```

编译和测试只验证源码。真实战斗结果及剩余缺口见 [战斗现状](combat.md)，不把协议夹具输出算作真人客户端验收。
