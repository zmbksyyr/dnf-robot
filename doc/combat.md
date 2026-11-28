# 状态包战斗

## 当前实现

Robot 接收真实队伍广播，维护当前房间的角色、怪物、位置和 HP；依据这些状态追击、朝向和选招，通过已有 UDP / relay 可靠传输发送动作与攻击对象。真人客户端的怪物控制方执行碰撞并产生伤害、死亡上报。

| 环节 | 源码入口 |
| --- | --- |
| 561 解封及通知分派 | `internal/protocol/dnf/robot_packets.go`、`crypt/anti_cipher.go` |
| 房间和怪物状态 | `party_dungeon_world.go`、`party_dungeon_motion.go` |
| 跟随、追击和目标选择 | `party_dungeon_follow.go`、`party_dungeon_navigation.go` |
| 技能状态、冷却与随机轮换 | `party_dungeon_skill.go`、`party_dungeon_skill_wire.go` |
| 普攻与男法攻击对象 | `party_dungeon_attack.go`、`party_dungeon_projectile.go` |
| PVF 技能与初始参数 | `internal/capability/pvf/pvf_skill*.go` |
| 创建角色时点满职业技能 | `internal/scheduler/repository/repository_robot_skills.go` |

561 是服务端 Anti 加密外包，内含选图、进房等通知；它不等同于队伍技能动作。已实现 1–19 类算法、尾部处理和 CRC 校验，真实 27 / 28 / 29 通知已有解密对照。解密实现是当前路线的必要依赖。

男法普攻在 state8 后按采集到的四个关键帧发送 24202 对象创建，命中或超时后销毁。其余职业不能仅替换对象编号就视为已支持。

新角色按 PVF 的职业、转职和技能等级上限初始化两套技能树，覆盖 11 个职业目录。运行时只选择已学会、参数已知、材料消耗为空且冷却就绪的候选；先随机选技能，再随机选该技能的状态，存在其他选择时避免连续重复。

## 已验证与待完成

2026-11-27 Windows 真人客户端 + Go Robot：

- 男法 Robot 自动击杀首房 8 / 8 怪物，真人未按攻击键；八次 TCP42 死亡报告与 TCP37 回复对应。
- 换房后 Robot 继续追击、普攻和造成伤害，随后撤退回城。
- 已验证新角色写入本职业满级技能树，并接收到服务端规范化的技能列表。

2026-11-28 Windows Native 与 Linux 原 ELF 各完成一次真人 + 剑士 Robot（job0 / Lv20）洛兰通关及回城。真人仅移动、选图和操作结束菜单，未按攻击键；两端均收到真实 type31 通关通知。Windows 保留首领 HP 161→0、成绩和奖励画面；Linux 保留 command81 内外校验通过的 HP_DAMAGE、首领倒地和通关菜单。Linux 诊断汇总省略了部分早期记录，不能据此给出整图逐怪明细。

Linux 跨宿主组队需公告可路由的 STUN / relay 地址，Robot TCP 本地地址也用于绑定队伍 UDP。真实 STUN 返回地址与 TCP 来源不同时，原游戏可能触发代理账号检查；本次以原有 `allow_proxy_user` 表仅授权测试真人账号，未伪造 NAT 状态。修改前值和数据库均已备份。

尚未完成：全职业全技能实际效果、Robot 受击与真实 HP / MP 结算同步、完整组队准备和异常边界。已移除近似受击和资源回复；USERINFO/PVF 装备与称号簿默认奖励仅是中间计算，缺少收集奖励、被动与运行时修正，不能初始化当前 HP/MP。职业武器倍率来自 PVF；付费技能要求已观测当前 MP，耗蓝规则或倍率未知时回退普攻。普通攻击本身不消耗 MP。旧 Windows 客户端崩溃尚无转储和根因，不能因本轮通关而称已修复。

已采到 SET_HP(12)、SET_MP(13)、HIT(19)、HP_DAMAGE(20)、USE_SKILL(25)。真人有受击记录，Robot 受击链尚未接通；不能据此宣称 Robot 资源已正确结算。

称号簿输入已接入 357 四类别条目、358 成就剩余计数、359 完成及 417 进度回复。仅保留本角色快照，未知与空收集分开；完成和成功取放称号后使缺少完整记录的类别失效，重选/退出清空。18 条保存的真实 TCP 包在双端按原密钥回放通过；收集奖励应用条件及最终属性仍待验证，当前 HP/MP 不由这些输入初始化。

证据在工作区 `x/acceptance-20261127`：

| 目录 | 内容 |
| --- | --- |
| `state-packet-cycle1` | 双真人客户端从进图到出图的完整状态包 |
| `state-combat-live1` | Robot 普攻实战原始包、`receipt.json`、`live-outcomes.json` |
| `state-combat-skills1` | 满技能初始化和随机选招阶段记录 |

本次双端通关证据在工作区 `x/evidence/V-61_win_*`、`V-61_linux_network_*`，汇总见 `x/report.md`。后续推进真实资源链及全职业对照；已通过的资格、冷却等行为引用既有结果。

## 废弃路线

协议探针、合成队长帧和直接死亡上报没有建立真实战斗闭环。完整原客户端、支援角色及 skillhost 原函数执行宿主依赖过重，已停止采用；未接入当前战斗的装备资格、护甲和材料结算模型一并移除。保留当前状态包链及其有效性能优化。
