# VM 部署与调试

使用 Python `paramiko` 进行 SSH、上传和远程命令；网络慢时允许重试。仅在用户明确要求时恢复快照。

## 实例

| 项目 | 值 |
| --- | --- |
| VM / SSH | `192.168.200.131`，`root / 123456` |
| Web | `http://192.168.200.131:8112`，密码 `twadmin` |
| Robot API / 队伍 UDP | `8111 / 5063` |
| Game / Monitor / Bridge | `10011 / 30303 / 7000` |
| Relay / Auction / Point | `7200 / 30803 / 30603` |
| 部署 / 主程序 / 配置 | `/root`、`/root/robot`、`/root/config` |
| 游戏 / 核心启停 | `/home/neople/game`、`/root/run`、`/root/stop` |
| 数据库 | `127.0.0.1:3306`，`game / uu5!^%jg`，主库 `d_taiwan` |

主配置为 `conf/config.ini`；机器人和市场配置为 `conf/robot_config.ini`、`conf/market_config.ini`。生成文件分别存入 `conf/templates/keys/pvf/state/logs/tmp`；game 的 RSA 文件和 Auction / Point 的 `iteminfo.dat` 是外部集成文件。

## 部署

1. 记录 git 提交，构建 Linux amd64 主程序并备份 `/root/robot`。
2. 上传到 `/root/robot.new`，正常停止 Robot 后替换主程序。
3. 将完整 `/root/config` 移到 `/root/config.bak.<timestamp>`，保留最新三份备份，新建空配置目录。由 Robot 重建文件，不自动迁移单个配置。
4. 启动后检查进程、`8111 / 8112` 及日志；用到组队、游戏或市场时检查对应服务端口。

普通 Restart 只重启进程，完整保留配置，不执行上述备份重置流程。

标准输出使用有界日志 sink：

```sh
mkdir -p /root/config/logs
nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/logs/stdout.log' >/dev/null 2>/root/config/logs/start_error.log &
```

外部安全服务的 GBK 配置 `/home/neople/secsvr/zergsvr/cfg/framework.xml` 使用大小轮转：`log_div_type_` / `bill_div_type_ = 101`，每份 104857600 字节、5 份。

## 核心恢复与稳定性

恢复核心前先停市场自动化、排空 Robot，再执行 `/root/stop` 并确认 Game / Monitor / Bridge 已停。通过脱离 SSH PTY 的 `setsid` 启动 `/root/run`；三个核心端口连续三次监听才算启动稳定。

已有稳定性脚本为 `/root/vm_random_stability.py`：

```sh
python2.7 /root/vm_random_stability.py 1
# 按需运行带时限的重复模式：
python2.7 /root/vm_random_stability.py 1 1h
```

结果写入 `/root/config/logs/stability/robot_stability_*`。从未完成阶段续跑，避免重验已完成项。数据库故障测试只断开已匹配到 Robot 的连接，不重启 MySQL；新 core 文件视为失败。脚本恢复测试数据并汇总 coverage、samples 和 summary。
