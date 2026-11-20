# 验收矩阵（自动生成，2026-11-21）

本表把"总体任务"的每一项与**可复核的证据**对应起来。证据来自实机日志、单元测试或基准，
列在每行后面；未能验证的项目单独列在末尾，并注明卡点。

## ① dnf-robot 地下城闭环

| 环节 | 状态 | 证据 |
|---|---|---|
| 城镇跟随 | 已实机 | `PARTY_AUTO_ACCEPT` / `PARTY_ROBOT_TQOS_READY`，机器人按队长位置移动 |
| 进副本 | 已实机 | 队长 `SelectDungeon(16)` → 机器人 `PARTY_DUNGEON_SKILL_PROFILE`，`dungeon=yes` |
| 自动寻怪 | 已实机 | `[PARTY_DUNGEON_NAV] active=true move=true reason=move` |
| 接近 | 已实机 | `[PARTY_DUNGEON_NAV] reason=in-range` |
| 零配置技能 | 已实机 | `[PARTY_DUNGEON_SKILL] skill=33 diehard / 26 watercannon / 4 dark_change / 1 windstrike`（仅开 `party_skill_catalog.json` 的 `enabled`） |
| 普攻连击 | 已实机 | `[PARTY_DUNGEON_ATTACK] step=0..3` |
| 击杀 | 已实机 | 队长侧 command-23 死亡 → 机器人停止攻击、`reason=in-range` 归队 |
| 清图判定 | 已实机 | 无活怪时 `active=false reason=idle-no-enemy`，不再攻击 |
| 跟随过图 | 已实机 | `MoveMap(1,0)` → `typ=29` 房间通知；机器人 `from=(…,500)` 用新房间坐标 |
| 新房间继续作战 | 已实机 | 新房间有真实怪物 id → 继续 `DUNGEON_ATTACK` + `DUNGEON_SKILL` |
| 副本退出/回城状态机 | 已实机 | 队长消失 → `PARTY_TRANSPORT_CLEARED`，随后新的城镇会话恢复 `TQOS_READY` |

## ② 三项目效率

| 项目 | 改动 | 测量 |
|---|---|---|
| dnf-robot | 帧路径零分配扫描；技能身份缓存；出站帧构造 | 340.1→255.9 ns/帧，336→80 B，7→1 次分配 |
| dnf-gm | dump 列元数据按表缓存；PVF 列目录/导出/`--pvf-nstr` | 470 表 741KB 导出 512→432–450 ms（−14%）；439220 条路径 937 ms |
| dnf-native | trap 站点 PC 直接映射缓存（`NR_SITE_BENCH` 自测） | 每次查表 4.7–10.3 → 2.5–3.5 ns（1.9–3.5×） |

## ③ 双平台

| 项目 | 证据 |
|---|---|
| dnf-robot | Linux（WSL, Go 1.22.5）与 Windows 各自 `go test ./...`：**45 个包全绿、0 失败** |
| dnf-robot 实机 | Linux 侧曾实测 20 个机器人上线（登录、组队、技能解析同 Windows） |
| dnf-native | 构建产出 i686 与 x86_64 两个 host；本机为 Windows 宿主 |
| dnf-gm | Windows 工具链构建与运行 |

## 未能验证（明确列出）

| 项目 | 卡点 |
|---|---|
| 服务端判定"通关 → 自动回城" | 需要服务端真的判定所有房间清空；夹具无法伪造。`GiveUpGame(42)` 三条路都因用户状态不在 {5,10,12,13} 被拒并断线 |
| Linux 侧副本实机 | Linux 版 game 在 WSL 下启动即 SIGSEGV（brk 堆把栈挤到 132 KB），环境级无解 |
| `git push` | 环境无 GitHub 凭据（无 PAT/SSH/gh 登录） |