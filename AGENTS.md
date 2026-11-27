# AI Rules

- Read documentation as UTF-8. Do not call Chinese text garbled.
- Before VM, deployment or debugging work, read `doc/vm.md`.
- Use Python `paramiko` for VM SSH, uploads and remote commands; do not use PowerShell `ssh` or `scp`.
- Restore VM snapshots only when the user requests it.
- Record the git commit and back up `/root/robot` before deployment; verify process, ports and logs afterward.
- Deploy only under `/root`. Keep generated Robot files under `/root/config/{conf,templates,keys,pvf,state,logs,tmp}`.
- Deployment backs up the complete config directory, keeps the newest three backups, and creates a fresh config directory. Do not migrate individual files automatically.
- Restart preserves the existing config directory; it must not run the deployment reset flow.
- Use the bounded stdout sink specified in `doc/vm.md`.
- Follow the state-packet combat route in `doc/combat.md`. Keep abandoned experiments to a short conclusion; reuse established evidence.
- Keep changes within this repository. Platform interfaces are documented in `doc/windows.md`.
