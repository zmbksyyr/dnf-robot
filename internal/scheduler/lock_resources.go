package scheduler

const (
	lockScopeScheduler = "scheduler"
	lockScopeConfig    = "config"

	lockResourceSchedulerOperation     = "operation"
	lockResourceSchedulerRuntimeStatus = "runtime-status"
	lockResourceSchedulerRandom        = "random-source"
	lockResourceSchedulerCleanup       = "cleanup-pending"
	lockResourceSchedulerStorePoints   = "store-points"
	lockResourceSchedulerStoreSlots    = "store-slots"
	lockResourceRobotConfig            = "robot"
)
