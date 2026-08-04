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
- For debug and long tests, use `/root/vm_random_stability.py`.

## VM

- IP: `192.168.200.131`
- SSH: `root / 123456`
- Web: `http://192.168.200.131:8112`
- Web password: `twadmin`

## Ports

- robot API: `8111`
- web: `8112`
- game: `10011`
- monitor: `30303`
- bridge: `7000`
- relay: `7200`
- party route 0 (UDP): `5063`
- auction: `30803`
- point: `30603`

## Paths

- deployment root: `/root` only; do not upload or start Robot from another directory
- robot binary: `/root/robot`
- config dir: `/root/config`
- main config: `/root/config/conf/config.ini`
- robot config: `/root/config/conf/robot_config.ini`
- market config: `/root/config/conf/market_config.ini`
- templates: `/root/config/templates/`
- keys: `/root/config/keys/`
- PVF exports: `/root/config/pvf/`
- runtime state: `/root/config/state/`
- main log: `/root/config/logs/robot.log`
- web/start log: `/root/config/logs/stdout.log`
- startup error log: `/root/config/logs/start_error.log` (truncated on each start)
- market log: `/root/config/logs/market.jsonl`
- temporary files: `/root/config/tmp/`
- start game services: `/root/run`
- stop game services: `/root/stop`
- game dir: `/home/neople/game`
- df_game_r: `/home/neople/game/df_game_r`

All Robot-owned generated files stay below `/root/config/{conf,templates,keys,pvf,state,logs,tmp}`. Game-directory RSA keys and Auction/Point `iteminfo.dat` files are external service integration files or published copies, not alternate Robot artifact roots; Robot-owned archives and standard sources remain under `/root/config/keys`, `/root/config/pvf`, and `/root/config/state/backups`.

## Robot Start

Use the bounded stdout sink so `stdout.log` follows the configured log limits:

```sh
mkdir -p /root/config/logs
nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/logs/stdout.log' >/dev/null 2>/root/config/logs/start_error.log &
```

Do not start with a direct `> /root/config/logs/stdout.log` redirect; that bypasses rotation.

The external security service uses `/home/neople/secsvr/zergsvr/cfg/framework.xml` (GBK). Keep `log_div_type_` and `bill_div_type_` at `101` (size based), with `max_*_file_size_ = 104857600` and `max_*_file_num_ = 5`; `205` is daily splitting and does not enforce the size limit.

## Core Services

Core recovery must first stop Market automation, drain Robot automation, run `/root/stop`, and confirm Game, Monitor, and Bridge are all down. Start `/root/run` outside the SSH PTY so closing the Paramiko channel cannot terminate its children:

```sh
mkdir -p /root/config/logs/stability
setsid sh -c 'cd /root && ./run' </dev/null >/root/config/logs/stability/core_start.log 2>&1
```

Do not accept startup from a single port sample. Game `10011`, Monitor `30303`, and Bridge `7000` must all remain listening for three consecutive samples. Point and Auction are checked by the Market readiness gate.

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
- Replace `/root/robot` from the staged binary (the launcher keeps the previous binary backup according to its normal policy).
- Move the complete `/root/config` to `/root/config.bak.<timestamp>`, retain only the newest three config backups, and create an empty `/root/config`. Do not migrate files into the new directory; robot regenerates them on startup.
- Start robot.
- If needed, start game services with `/root/run`.
- Verify `8111`, `8112`, logs, and process.
- If party routing is needed, verify the UDP `5063` listener.
- If game is needed, verify stable `10011`, `30303`, and `7000` listeners.
- If market is needed, verify `30803` and `30603`.

The launcher's Restart operation only stops and starts `/root/robot`. It must preserve `/root/config` exactly and must not run the deployment backup/reset flow.

## Stability Test

Run on the VM:

```sh
python2.7 /root/vm_random_stability.py 1
# Optional repeated/fuzz mode with a deadline:
python2.7 /root/vm_random_stability.py 1 1h
```

The default single round runs two dependency-ordered matrices. `integrated_load_data_matrix` keeps one 600-user observation window across natural stalls, party/relay checks, normal market operations, market service/source faults, and database faults. `restart_recovery_matrix` covers broken config fallback plus Web, Monitor, Core, Bridge, compatibility guard, and key recovery. Related faults share one scoped recovery, market services start in parallel without using auto restock as a trigger, and Core restart is a single controlled stop/start accepted only after stable Game, Monitor, and Bridge samples. Resume an old subphase name such as `--start-scenario database_fault_matrix` or `--start-scenario runtime_recovery_matrix` without rerunning earlier work; a resumed run only enforces coverage owned by the executed subphases.

The database connection fault never restarts MySQL. It maps `/proc/<robot-pid>/fd` socket inodes through `/proc/net/tcp*` to `information_schema.PROCESSLIST`, runs `KILL CONNECTION` only for the matched Robot connections, then requires every old connection to disappear, a replacement connection to appear, and Robot `Ping` plus `SELECT 1` to succeed. This preserves Bridge and unrelated game-service connections.

At test start the script snapshots `/home/neople/*/core.*` and `/home/dxf/*/core.*`. A new path or a replaced file at an existing path fails its phase, is recorded in coverage and `summary.json.new_core_dumps`, and is checked again during final recovery. Pre-existing unchanged files are ignored. The script backs up and restores destructive test data, writes coverage, failures, samples, and a summary under `/root/config/logs/stability/robot_stability_*`, and exits nonzero when any invariant fails or the requested run does not complete.
