# AI Rules

Before any VM, deploy, or debug task, read:

- `doc/vm.md`

Must follow:

- Read docs as UTF-8.
- Do not call Chinese text garbled.
- Use Python `paramiko` for VM SSH, upload, and remote commands.
- Do not use PowerShell `ssh` or `scp` for VM work.
- Do not restore VM snapshots unless the user asks.
- Deploy only after recording the git commit and backing up `/root/robot`.
- After deploy, check process, ports, and logs.
- Use the stability pressure script for debug and long tests. Do not use old manual debug docs.

Fast VM card:

- VM: `192.168.200.131`
- SSH: `root / 123456`
- Web: `http://192.168.200.131:8112`
- Web password: `twadmin`
- robot API: `8111`
- game: `10011`
- auction: `30803`
- point: `30603`
- deployment root: `/root` only
- robot: `/root/robot`
- config: `/root/config`
- main config: `/root/config/conf/config.ini`
- runtime logs: `/root/config/logs/`
- templates, keys, PVF, state, and temporary files: `/root/config/templates/`, `/root/config/keys/`, `/root/config/pvf/`, `/root/config/state/`, `/root/config/tmp/`

Every deployment moves the complete `/root/config` to `/root/config.bak.<timestamp>`, keeps only the newest three config backups, creates a fresh `/root/config`, and lets robot regenerate all files. Do not migrate individual files automatically; users recover anything needed from the backup.

All Robot-owned generated files stay below `/root/config/{conf,templates,keys,pvf,state,logs,tmp}`. Game RSA files and Auction/Point `iteminfo.dat` are external integration files/copies, not alternate deployment roots. A normal Restart only restarts `/root/robot` and must not move, delete, or recreate `/root/config`.

Start robot with the bounded stdout sink:

```sh
mkdir -p /root/config/logs
nohup sh -c '/root/robot 2>&1 | /root/robot --bounded-log-sink /root/config/logs/stdout.log' >/dev/null 2>/root/config/logs/start_error.log &
```
