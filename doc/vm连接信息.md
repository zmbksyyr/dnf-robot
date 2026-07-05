# VM Connection Rules

This file is for AI agents and maintainers.

## Must Follow

- This file is UTF-8 text. Do not call it garbled.
- Read docs with UTF-8.
- Before any VM, deploy, or debug task, read this file, `tools/随机稳定性压测脚本.md`, and `tools/vm_random_stability.py`.
- Use Python `paramiko` for SSH, upload, and remote commands.
- Do not use PowerShell `ssh` or `scp` for VM work.
- Use `vmrun` only for VM snapshot/start operations.
- VM network can be slow. Wait and retry before saying deploy failed.
- Do not restore a VM snapshot unless the user asks for it.
- Before deploy, record the git commit.
- Before deploy, back up `/root/robot`.
- After deploy, check process, ports, and logs.
- Use the stability pressure script for debug and long tests. Do not use old manual debug docs.

## Quick Card

- VM IP: `192.168.200.131`
- SSH: `root / 123456`
- Web: `http://192.168.200.131:8112`
- Web password: `twadmin`
- robot TCP API: `8111`
- game port: `10011`
- auction port: `30803`
- point port: `30603`
- robot binary: `/root/robot`
- config dir: `/root/config`
- main config: `/root/config/config.ini`
- robot config: `/root/config/robot_config.ini`
- main log: `/root/config/log_robot`
- web/start log: `/root/robot_stdout.log`
- market log: `/root/config/market_log.jsonl`
- start game services: `/root/run`
- stop game services: `/root/stop`
- game dir: `/home/neople/game`
- df_game_r: `/home/neople/game/df_game_r`

## Database

- host: `127.0.0.1`
- port: `3306`
- user: `game`
- password: `uu5!^%jg`
- main db: `d_taiwan`
- auction db: `taiwan_cain_auction_gold`
- cera db: `taiwan_cain_auction_cera`

Example:

```sh
MYSQL_PWD='uu5!^%jg' mysql -ugame -N -B -e "SELECT 1;"
```

## VMware

- VMX: `D:\cache\game\DNF_85_CUICAN_20260407_璀璨85完结版\DNFServer\DNFServer 7.3 x64.vmx`
- vmrun: `C:\Program Files (x86)\VMware\VMware Workstation\vmrun.exe`
- last snapshot: `快照 2`

Snapshot commands:

```text
vmrun -T ws revertToSnapshot "<VMX>" "快照 2"
vmrun -T ws start "<VMX>" nogui
```

## Local Build

Run from repo root:

```powershell
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go test ./...
go build -trimpath -ldflags='-s -w' -o dist\robot-linux-amd64 .\cmd\robot
```

## Deploy Steps

1. Record local commit: `git rev-parse --short HEAD`.
2. Build Linux amd64 robot.
3. Upload binary to `/root/robot.new` with `paramiko`.
4. Run `chmod +x /root/robot.new`.
5. Back up old `/root/robot`.
6. Stop old `/root/robot` process.
7. Replace `/root/robot`.
8. Start robot:

```sh
nohup /root/robot >/root/robot_stdout.log 2>&1 &
```

9. Check `8111` and `8112`.
10. If game services are needed, run `/root/run`.
11. Check `10011`, `30803`, and `30603`.
12. Check logs for `panic` and `fatal`.

## Quick Checks

```sh
ps -eo pid,lstart,cmd | grep -E '(/root/robot|df_game_r|df_auction_r|df_point_r)' | grep -v grep
ss -lntp | grep -E ':(8111|8112|10011|30803|30603)'
tail -n 80 /root/robot_stdout.log
tail -n 80 /root/config/log_robot
tail -n 80 /root/config/market_log.jsonl
```

## Stability Test

Use this script instead of old manual debug docs:

- local source: `tools/vm_random_stability.py`
- VM path: `/root/vm_random_stability.py`
- call guide: `tools/随机稳定性压测脚本.md`

Common VM command:

```sh
python /root/vm_random_stability.py --hours 1
```

## Deploy Must Pass

- `/root/robot` process exists.
- Port `8111` is listening.
- Port `8112` is listening.
- `/root/robot_stdout.log` has no fresh `panic` or `fatal`.
- `/root/config/log_robot` is being updated.
- If game is started, port `10011` is listening.
- If market is started, ports `30803` and `30603` are listening.

## Packaging

- Commit first.
- Put the zip on Desktop.
- Name format: `dofrobot-main_YYYYMMDD-HHMMSS_feature.zip`.
- Include source, `.git`, `doc`, default resources, and built binary.
- Do not include temp logs, old zip files, or old diagnostic output.
