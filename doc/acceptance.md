# 验收矩阵（2026-11-21 修订）

本表把"总体任务"的每一项与**可复核的证据**对应起来。2026-11-21 第 75–77 轮对"副本是否真的进入"
做了更正，本版按更正后的事实重写。

## ① dnf-robot 地下城闭环

机器人侧的**逻辑**逐项验证（用夹具合成的队长队伍帧驱动，等价于真实队长的动作流）：

| 环节 | 状态 | 证据 |
|---|---|---|
| 城镇跟随 | 已验证 | `PARTY_AUTO_ACCEPT` / `PARTY_ROBOT_TQOS_READY`，按队长位置移动 |
| 进副本（机器人侧响应） | 已验证 | 队长 `SelectDungeon(16)` → 机器人 `PARTY_DUNGEON_SKILL_PROFILE` |
| 自动寻怪 | 已验证 | `[PARTY_DUNGEON_NAV] active=true move=true reason=move` |
| 接近 | 已验证 | `[PARTY_DUNGEON_NAV] reason=in-range` |
| 零配置技能 | 已验证 | `[PARTY_DUNGEON_SKILL] diehard / watercannon / dark_change / windstrike`（仅打开 `enabled`） |
| 普攻连击 | 已验证 | `[PARTY_DUNGEON_ATTACK] step=0..3` |
| 击杀 | 已验证 | 队长侧 command-23 死亡 → 机器人停手、归队 |
| 清图判定 | 已验证 | `active=false reason=idle-no-enemy` |
| 换房（协议） | 部分验证 | 修正 MoveMap 报文后服务端有应答（`typ=48`/`typ=29`），但**未在真实多房间副本中验证** |
| 回城跟随（状态机） | 已验证 | 队长消失 → `PARTY_TRANSPORT_CLEARED`；新城镇会话恢复 `TQOS_READY` |

**关键限制**：只有**教程副本 10000** 是服务端真实进入的（`SetPos` 被拒可证）。普通副本（id 1）在
`SelectDungeon` 被世界地图校验拒绝（应答字节 8），用户状态退回城镇，所以"真实多房间副本"的
进图/换房/通关回城**没有实机验证**。

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
| dnf-native | 构建产出 i686 与 x86_64 两个 host |
| dnf-gm | Windows 工具链构建与运行 |

## 未验证（明确列出）

| 项目 | 卡点 |
|---|---|
| 普通副本真实进图 | `SelectDungeon` 被世界地图校验拒回错误 8；`GetWorldMapIndex` 只在 areaType==4 的区域有值，而 SetArea 是否生效无法观测（区域移动日志只记公会基地） |
| 真实多房间换房 | 依赖上一条；教程图只有一张图，`(1,0)` 等格全部被拒 |
| 服务端"通关 → 自动回城" | 同上；`GiveUpGame(42)` 三条路都因用户状态不在 {5,10,12,13} 被断线 |
| Linux 侧副本实机 | Linux 版 game 在 WSL 启动即 SIGSEGV（brk 堆把栈挤到 132 KB） |
| `git push` | 环境无 GitHub 凭据 |