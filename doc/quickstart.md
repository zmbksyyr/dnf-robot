# 快速上手（Quickstart）

本文把"怎么把东西跑起来、怎么验证"压缩成可直接复制的命令。
所有临时产物都在 `D:\cache\dnf\x`，三个仓库在 `D:\cache\dnf\src`。

## 一、Windows 实例（最常用）

### 启动

```powershell
python -B D:\cache\dnf\x\live\restart_live.py     # 起 MySQL + 游戏服务端 + 代理
python -B D:\cache\dnf\x\live\enable_skills.py    # 打开"零配置技能"开关
```

### 一键验收（六段）

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File D:\cache\dnf\x\live\full_loop.ps1
```

期望输出末尾：

```
town_follow  enter_and_attack  clear_room  leave_dungeon  town_follow_again  zero_config_skill
ALL PASS
```

### 关闭

```powershell
python -B D:\cache\dnf\x\live\close_gui.py
```

## 二、Linux 服务端（WSL）

> 注意：Linux 服务端与 Windows 实例**不能同时运行**（都占 10011 / 3306）。先关掉 Windows 实例。

```powershell
python -B D:\cache\dnf\x\live\close_gui.py
```

```bash
# 1) 起 MySQL + secagent
sh /mnt/d/cache/dnf/x/wsl/start_mysql5.sh
sh /mnt/d/cache/dnf/x/wsl/start_sec.sh

# 2) 起服务栈 + 直接执行游戏（关键：service 栈 + 直接执行）
setsid /bin/sh -c 'sh /mnt/d/cache/dnf/x/wsl/run_direct.sh' < /dev/null > /root/logs/run_direct.log 2>&1 &

# 3) 等待并确认
sleep 90; ss -lntp | grep 10011
```

看到 `0.0.0.0:10011` 监听 + 日志里 `The siroco11 channel 11('ch.11') server has been started successfully.` 即为成功。

### 用协议夹具连它

```bash
cd /mnt/d/cache/dnf/src/dnf-robot
export PATH=/usr/local/go1.22/bin:$PATH GOPROXY=https://goproxy.cn
export DNF_LIVE_DSN="game:uu5!^%jg@tcp(127.0.0.1:3306)/d_taiwan"
export DNF_LIVE_KEY=/home/neople/game/privatekey.pem
export DNF_LIVE_UID=17000020 DNF_LIVE_CID=1 DNF_LIVE_SLOT=0
export DNF_LIVE_GAME=127.0.0.1:10011 DNF_LIVE_DUNGEON=0
go test -tags live -run TestLiveDungeonProbe -v -timeout 130s ./internal/protocol/dnf/
```

### 停止

```bash
pkill -9 -f df_game_r; pkill -9 -f 'ld-linux'; pkill -9 -f secagent; pkill -9 -f mysqld
```

## 三、dnf-gm 验证

```powershell
$env:DNF_GM_BUILD = "D:\cache\dnf\x\gm-build"
& "$env:DNF_GM_BUILD\DNF_GM.exe" "--db-test"                       # 连库自检
& "$env:DNF_GM_BUILD\DNF_GM.exe" "--db-dump=D:\cache\dnf\x\d.sql"  # 整库导出
& "$env:DNF_GM_BUILD\DNF_GM.exe" "--pvf-list=D:\cache\dnf\x\list.txt"
& "$env:DNF_GM_BUILD\DNF_GM.exe" "--pvf-extract=路径:输出文件"
```

## 四、dnf-native 验证

```powershell
cd D:\cache\dnf\src\dnf-native
python -B tools\build.py
python -B tests\test_elf.py; python -B tests\test_vm.py; python -B tests\test_files.py
python -B tests\test_net.py; python -B tests\test_poll.py; python -B tests\test_sysinfo.py
```

## 五、dnf-robot 测试

```powershell
cd D:\cache\dnf\src\dnf-robot
go test ./...      # 45 个包
go vet ./...
```

Linux 侧（WSL，Go 1.22）：

```bash
cd /mnt/d/cache/dnf/src/dnf-robot && PATH=/usr/local/go1.22/bin:$PATH GOPROXY=https://goproxy.cn go test ./...
```

## 六、常见问题

| 现象 | 处理 |
|---|---|
| 端口 10011 被占 | Windows 实例与 WSL 服务端互斥，先停对方 |
| 机器人登录 `reason=36` | 角色背包数据异常，需要修复 inventory（见 `doc/windows.md` 记录） |
| 验收脚本某段 FAIL | 先 `restart_live.py` 再跑，通常是实例状态残留 |
| Linux 侧选图无应答 | 已知问题，见 `doc/final-report.md` 的未完成项 |