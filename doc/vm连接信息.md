# VM Rules For AI

Read this before any VM, deploy, or debug task.

## Hard Rules

- This file is UTF-8 text. Do not call it garbled.
- Use Python `paramiko` for SSH, upload, and remote commands.
- Do not use PowerShell `ssh` or `scp` for VM work.
- VM network can be slow. Wait and retry before declaring failure.
- VM snapshot restore is allowed only when the user asks for it.
- Before deploy, record the git commit and back up `/root/robot`.
- After deploy, verify process, ports, and logs.
- For debug and long tests, use `tools/vm_random_stability.py`.

## VM

- IP: `192.168.200.131`
- SSH: `root / 123456`
- Web: `http://192.168.200.131:8112`
- Web password: `twadmin`

## Ports

- robot API: `8111`
- web: `8112`
- game: `10011`
- auction: `30803`
- point: `30603`

## Paths

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

## Deploy Must Do

- Build Linux amd64 robot.
- Upload to `/root/robot.new`.
- Back up `/root/robot`.
- Replace `/root/robot`.
- Start robot.
- If needed, start game services with `/root/run`.
- Verify `8111`, `8112`, logs, and process.
- If game is needed, verify `10011`.
- If market is needed, verify `30803` and `30603`.

## Stability Test

- local script: `tools/vm_random_stability.py`
- VM script: `/root/vm_random_stability.py`
- guide: `tools/随机稳定性压测脚本.md`
