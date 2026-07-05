# 代码整理审计

## 当前阶段

- 阶段：二，统一状态表和锁机制。
- 状态：进行中。
- 本轮目标：把现有层级纳入架构测试，防止新增反向依赖和未知顶层包；开始收敛锁资源命名。

## 层级归属

```text
internal/bootstrap     启动、装配、运行时预检
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
```

下一步继续审计：

```text
lockhub raw field names
```

## 状态来源

当前已收敛：

```text
OperationStatus.State -> capability/robot OperationState* 常量
RuntimeStatus.StateName -> capability/robot RuntimeState* 常量
SchedulerStatus.Mode -> capability/robot SchedulerMode* 常量
Actor Snapshot.State -> actor State* 常量
marketapp JobSummary.Status -> capability/marketapp MarketJobStatus* 常量
marketapp MarketServiceStatus.Status -> capability/marketapp MarketServiceStatus* 常量
marketapp MarketPolicyStatus.Health -> capability/marketapp marketPolicyHealth* 常量
marketapp MarketPolicyStatus.Mode -> capability/marketapp marketPolicyMode* 常量
ActionResult.State -> capability/robot ActionState* 常量
```

下一步继续审计：

```text
lockhub resource names
```

## Legacy 白名单

这些不是目标形态，只是当前运行路径暂时保留：

```text
internal/protocol/auctionapp/executor.go -> capability/marketapp
internal/protocol/dnfruntime/runtime.go  -> capability/keypair, capability/robot
```

下一轮不能扩大白名单，只能减少白名单。

## 当前发现

- `internal/bootstrap` 已纳入架构矩阵。
- protocol 仍有两个反向依赖 capability 的历史点。
- scheduler 根包仍有 `database/sql` 聚合点，后续应继续向 repository 边界收敛。
- marketapp 是最大功能岛，后续需要单独做状态和自愈审计。
- scheduler/repository 的锁资源名已经集中为常量，后续不允许重新散落字符串。
- marketapp 的 App 内部锁已经按职责命名为 stateMu/jobMu/autoMu，不再保留泛化 mu 字段。
- actor 的 Actor/Ledger 内部锁已经按职责命名为 stateMu/indexMu；uid 动作串行锁保持 uidLocks。
- OperationStatus 状态已经从 scheduler 字符串收敛到 capability/robot 常量。
- RuntimeStatus 状态名已经从 actor/scheduler 字符串判断收敛到 capability/robot 常量。
- SchedulerStatus 模式已经从 scheduler 字符串值收敛到 capability/robot 常量。
- Actor Snapshot.State 已经在 actor 层内收敛为 State 类型和 State* 常量。
- marketapp JobSummary 状态已经从 restock/collect 字符串收敛到 MarketJobStatus 常量。
- marketapp MarketServiceStatus 状态已经从服务检测字符串赋值收敛到 MarketServiceStatus 常量。
- marketapp MarketPolicyStatus 健康状态已经从字符串赋值收敛到 marketPolicyHealth 常量。
- marketapp MarketPolicyStatus 模式已经从自愈策略字符串赋值收敛到 marketPolicyMode 常量。
- ActionResult.State 已经从 actor/scheduler/robotaction/store 的动作结果字符串收敛到 capability/robot ActionState 常量。

## 本轮验证

```text
go test ./internal/architecture
go test ./...
```

## 下一轮目标

继续阶段二：统一状态表和锁机制。

优先扫描：

```text
actor runtime status
scheduler status
market status
operation status
service status
DB facts
lockhub resource names
```
