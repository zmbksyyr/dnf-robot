# 代码整理审计

## 当前阶段

- 阶段：三，边界与功能岛收口。
- 状态：进行中。
- 本轮目标：继续减少 legacy 反向依赖，压实 composition 装配层，审计 marketapp 功能岛和自愈边界。

## 层级归属

```text
internal/bootstrap     启动、装配、运行时预检
internal/composition   跨层组合、协议实现注入能力接口
internal/entry         Web/TCP 入口适配
internal/scheduler     调度、计划、actor 分配、能力调用
internal/actor         actor 生命周期和任务执行边界
internal/capability    功能岛
internal/protocol      协议连接、发包、收包、协议结构
internal/foundation    配置、锁、日志、SQL 基础设施、网络基础设施
internal/shared        跨层 DTO
```

## 已固定规则

- 顶层 `internal/*` 目录必须属于已知层。
- `bootstrap` 不依赖 `entry/scheduler/actor/protocol`。
- `composition` 只做跨层组合，允许依赖 `capability/protocol/foundation/shared`，不承载业务策略。
- `scheduler` 不依赖 `protocol`。
- `capability` 不依赖 `protocol/scheduler/entry`。
- `entry` 不依赖 `protocol`。
- `actor` 不依赖 `scheduler/entry`。
- `foundation` 不依赖业务层。
- `protocol` 不依赖 `scheduler/entry`，并且默认不允许依赖 `capability`。
- 现有 protocol 反向依赖 capability 必须进入显式 legacy 白名单，不能继续扩散。
- scheduler 和 scheduler/repository 不能直接写锁资源 scope 字面量，必须使用命名常量。

## 锁资源命名

当前已收敛：

```text
config:robot
scheduler:operation
scheduler:runtime-status
scheduler:random-source
scheduler:cleanup-pending
scheduler:store-points
scheduler:store-slots
repository:schema-cache
marketapp:state
marketapp:job
marketapp:auto
actor:state
actor:index
uid:action
store:point
```

下一步继续审计：protocol 剩余 DTO 反向依赖、marketapp 自愈状态、压测覆盖面。

## 状态来源

当前已收敛：

```text
OperationStatus.State -> capability/robot OperationState* 常量
RuntimeStatus.StateName -> shared RuntimeState* 常量，capability/robot 只保留别名
SchedulerStatus.Mode -> capability/robot SchedulerMode* 常量
Actor Snapshot.State -> actor State* 常量
marketapp JobSummary.Status -> capability/marketapp MarketJobStatus* 常量
marketapp MarketServiceStatus.Status -> capability/marketapp MarketServiceStatus* 常量
marketapp MarketPolicyStatus.Health -> capability/marketapp marketPolicyHealth* 常量
marketapp MarketPolicyStatus.Mode -> capability/marketapp marketPolicyMode* 常量
marketapp LogEvent.Status -> capability/marketapp marketLogStatus* 常量
ActionResult.State -> capability/robot ActionState* 常量
StatusItem.DBState -> capability/robot DBState* 常量
```

下一步继续审计：marketapp 日志状态、运行事实和自愈决策输入。

## Legacy 白名单

当前没有 protocol -> capability 反向依赖白名单。后续不能新增白名单。

## 当前发现

- `internal/bootstrap` 已纳入架构矩阵。
- protocol 反向依赖 capability 的历史点已经清空。
- auctionapp 执行器已经从 protocol 移到 composition，作为协议和市场能力之间的装配桥。
- dnfruntime 已经去掉 keypair 反向依赖，登录 key 由启动层注入。
- dnfruntime 运行态 DTO 已经下沉到 shared，protocol 不再依赖 capability/robot。
- scheduler 根包已经不再 import `database/sql`；SQL repository 由启动层装配，具体实现留在 scheduler/repository。
- marketapp 是最大功能岛，后续需要单独做状态和自愈审计。
- scheduler/repository 的锁资源名已经集中为常量，后续不允许重新散落字符串。
- marketapp 的 App 内部锁已经按职责命名为 stateMu/jobMu/autoMu，不再保留泛化 mu 字段。
- actor 的 Actor/Ledger 内部锁已经按职责命名为 stateMu/indexMu；uid 动作串行锁保持 uidLocks。
- store 点位协调器内部锁已经按职责命名为 pointMu。
- OperationStatus 状态已经从 scheduler 字符串收敛到 capability/robot 常量。
- RuntimeStatus 状态名已经下沉到 shared RuntimeState 常量，capability/robot 保留别名供业务层使用。
- SchedulerStatus 模式已经从 scheduler 字符串值收敛到 capability/robot 常量。
- Actor Snapshot.State 已经在 actor 层内收敛为 State 类型和 State* 常量。
- marketapp JobSummary 状态已经从 restock/collect 字符串收敛到 MarketJobStatus 常量。
- marketapp MarketServiceStatus 状态已经从服务检测字符串赋值收敛到 MarketServiceStatus 常量。
- marketapp MarketPolicyStatus 健康状态已经从字符串赋值收敛到 marketPolicyHealth 常量。
- marketapp MarketPolicyStatus 模式已经从自愈策略字符串赋值收敛到 marketPolicyMode 常量。
- marketapp LogEvent 和清理结果状态已经从散落字符串收敛到 marketLogStatus 常量。
- ActionResult.State 已经从 actor/scheduler/robotaction/store 的动作结果字符串收敛到 capability/robot ActionState 常量。
- StatusItem.DBState 已经从 scheduler 展示字符串收敛到 capability/robot DBState 常量。

## 本轮验证

```text
go test ./internal/architecture
go test ./...
```

## 下一轮目标

继续阶段三：边界与功能岛收口。

优先扫描：

```text
DB facts
protocol legacy imports
market self-healing policy
market log/event status
stress coverage gaps
```
