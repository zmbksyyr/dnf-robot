# 代码整理审计

## 当前阶段

- 阶段：一，立规则和依赖审计。
- 状态：进行中。
- 本轮目标：把现有层级纳入架构测试，防止新增反向依赖和未知顶层包。

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

## 本轮验证

```text
go test ./internal/architecture
```

## 下一轮目标

阶段二：统一状态表和锁机制。

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
