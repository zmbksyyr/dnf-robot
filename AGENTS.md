# AI Rules

Before any VM, deploy, or debug task, read:

- `doc/vm连接信息.md`
- `tools/随机稳定性压测脚本.md`
- `tools/vm_random_stability.py`

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
- robot: `/root/robot`
- config: `/root/config`
