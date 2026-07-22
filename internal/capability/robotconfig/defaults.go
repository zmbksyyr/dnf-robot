package robotconfig

func Default() RuntimeConfig {
	return RuntimeConfig{
		LevelMin: 50, LevelMax: 85, Jobs: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, GrowTypes: []int{0, 1, 2},
		RobotUIDStart:     17000000,
		RobotUIDEnd:       17000999,
		RobotUIDGuard:     17999999,
		NameASCIIFallback: false, NameASCIIPrefix: "twbot",
		SpawnFixed: false, SpawnVillage: 3, SpawnFallbackVillage: 1, SpawnArea: 0, SpawnXMin: 240, SpawnXMax: 1800, SpawnYMin: 180, SpawnYMax: 460,
		MoveSpeedMin: 180, MoveSpeedMax: 260, MoveType: 5, MoveSteps: 4, MoveStepDelayMS: 1200,
		LoginDelayMS: 1000, ReconnectDelayMS: 5000, MaxReconnect: 2, MaxOnlineRobots: 1000, MaxOnlinePerCommand: 1000, OnlineDispatchIntervalMS: 1000, OnlineConfirmTimeoutMS: 90000,
		DefaultMoney: 1000000, DefaultCoin: 5, InventoryCapacity: 16,
		EquipSlots: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, EquipRarityMin: 0, EquipRarityMax: 5, EquipIntensifyMin: 7, EquipIntensifyMax: 10, EquipSmithingMin: 0, EquipSmithingMax: 8,
		PreferEquipSets: true, EquipSetMinSlots: 2,
		AvatarSlots: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, MinAvatarSlots: 10, PreferAvatarSets: true, AvatarSetMinSlots: 2,
		StoreItemSlots: 1, StoreItemCountMin: 1, StoreItemCountMax: 1, StorePriceMin: 100000, StorePriceMax: 5000000, StoreInventoryStartBox: 7, StoreItemAllowIDs: []int{3037, 3031, 3032, 3034, 3035}, StoreItemDenyIDs: []int{7312, 7404, 7560, 7563, 7567, 7746},
		StoreConfirmTimeoutSec: 30,
		FollowRadiusX:          120, FollowRadiusY: 30, ShoutDelayMS: 1000, ShoutSendEnabled: true,
		AutoActions: true, AutoTargetOnlineCount: 20,
		AutoMoveIntervalMinSec: 6, AutoMoveIntervalMaxSec: 18, AutoShoutIntervalMinSec: 45, AutoShoutIntervalMaxSec: 120,
		AutoStoreProbabilityPercent: 5, AutoStoreIntervalMinSec: 120, AutoStoreIntervalMaxSec: 180, AutoStoreDurationSec: 120, AutoStoreTickSec: 10, AutoStoreMaxPositionTries: 10, AutoStoreFailCooldownSec: 60,
		AutoGamePortStableSec: 15, AutoGamePortCheckTimeoutMS: 800,
		SchedulerBadRecoverSec: 60, SchedulerBadFailures: 3, SchedulerMetricsIntervalSec: 10, SchedulerStoreConcurrent: 30, SchedulerOnlineBatchSize: 120, SchedulerOnlineStartRate: 20, SchedulerOnlineFillTimeout: 120,
		SchedulerBreakerAbnormalPct: 30, SchedulerBreakerPauseSec: 300, SchedulerBreakerReleaseBatch: 20, SchedulerBreakerFloorPct: 70, SchedulerPortDownReleaseBatch: 20,
		SystemActorPollMS: 3000, SystemManualActionTimeoutSec: 60, SystemPacketRatePerSec: 20,
	}
}

func Normalize(rc *RuntimeConfig) {
	if rc.MaxOnlineRobots <= 0 {
		rc.MaxOnlineRobots = 1000
	}
	if rc.MaxOnlinePerCommand <= 0 || rc.MaxOnlinePerCommand > rc.MaxOnlineRobots {
		rc.MaxOnlinePerCommand = rc.MaxOnlineRobots
	}
	if rc.OnlineDispatchIntervalMS < 0 {
		rc.OnlineDispatchIntervalMS = 1000
	}
	if rc.OnlineConfirmTimeoutMS < 5000 {
		rc.OnlineConfirmTimeoutMS = 5000
	}
	if rc.LoginDelayMS < 1000 {
		rc.LoginDelayMS = 1000
	}
	if rc.ReconnectDelayMS < 5000 {
		rc.ReconnectDelayMS = 5000
	}
	if rc.MaxReconnect < 0 {
		rc.MaxReconnect = 0
	}
	if rc.MaxReconnect > 10 {
		rc.MaxReconnect = 10
	}
	if rc.ShoutDelayMS < 0 {
		rc.ShoutDelayMS = 0
	}
	if rc.MoveSteps <= 0 {
		rc.MoveSteps = 4
	}
	if rc.MoveSteps > 12 {
		rc.MoveSteps = 12
	}
	if rc.MoveStepDelayMS < 0 {
		rc.MoveStepDelayMS = 0
	}
	if rc.InventoryCapacity <= 0 {
		rc.InventoryCapacity = 16
	}
	if len(rc.Jobs) == 0 {
		rc.Jobs = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	}
	if len(rc.GrowTypes) == 0 {
		rc.GrowTypes = []int{0, 1, 2}
	}
	if rc.RobotUIDStart < 100000 {
		rc.RobotUIDStart = 17000000
	}
	if rc.RobotUIDEnd < rc.RobotUIDStart {
		rc.RobotUIDEnd = rc.RobotUIDStart + 999
	}
	if rc.EquipRarityMax < rc.EquipRarityMin {
		rc.EquipRarityMin, rc.EquipRarityMax = rc.EquipRarityMax, rc.EquipRarityMin
	}
	if rc.EquipIntensifyMax < rc.EquipIntensifyMin {
		rc.EquipIntensifyMin, rc.EquipIntensifyMax = rc.EquipIntensifyMax, rc.EquipIntensifyMin
	}
	if rc.EquipSmithingMax < rc.EquipSmithingMin {
		rc.EquipSmithingMin, rc.EquipSmithingMax = rc.EquipSmithingMax, rc.EquipSmithingMin
	}
	if rc.EquipSetMinSlots <= 1 {
		rc.EquipSetMinSlots = 2
	}
	if len(rc.EquipSlots) > 0 && rc.EquipSetMinSlots > len(rc.EquipSlots) {
		rc.EquipSetMinSlots = len(rc.EquipSlots)
	}
	if rc.MinAvatarSlots < 0 {
		rc.MinAvatarSlots = 0
	}
	if rc.AvatarSetMinSlots <= 1 {
		rc.AvatarSetMinSlots = 2
	}
	if len(rc.AvatarSlots) > 0 && rc.AvatarSetMinSlots > len(rc.AvatarSlots) {
		rc.AvatarSetMinSlots = len(rc.AvatarSlots)
	}
	if rc.AutoMoveIntervalMinSec <= 0 {
		rc.AutoMoveIntervalMinSec = 6
	}
	if rc.AutoMoveIntervalMaxSec < rc.AutoMoveIntervalMinSec {
		rc.AutoMoveIntervalMaxSec = rc.AutoMoveIntervalMinSec + 8
	}
	if rc.AutoShoutIntervalMinSec <= 0 {
		rc.AutoShoutIntervalMinSec = 45
	}
	if rc.AutoShoutIntervalMaxSec < rc.AutoShoutIntervalMinSec {
		rc.AutoShoutIntervalMaxSec = rc.AutoShoutIntervalMinSec + 60
	}
	if rc.AutoTargetOnlineCount < 0 {
		rc.AutoTargetOnlineCount = 0
	}
	if rc.AutoTargetOnlineCount > rc.MaxOnlineRobots {
		rc.AutoTargetOnlineCount = rc.MaxOnlineRobots
	}
	if rc.AutoGamePortStableSec <= 0 {
		rc.AutoGamePortStableSec = 15
	}
	if rc.AutoGamePortStableSec > 300 {
		rc.AutoGamePortStableSec = 300
	}
	if rc.AutoGamePortCheckTimeoutMS <= 0 {
		rc.AutoGamePortCheckTimeoutMS = 800
	}
	if rc.AutoGamePortCheckTimeoutMS > 10000 {
		rc.AutoGamePortCheckTimeoutMS = 10000
	}
	if rc.AutoStoreProbabilityPercent < 0 {
		rc.AutoStoreProbabilityPercent = 0
	}
	if rc.AutoStoreProbabilityPercent > 100 {
		rc.AutoStoreProbabilityPercent = 100
	}
	if rc.AutoStoreIntervalMinSec <= 0 {
		rc.AutoStoreIntervalMinSec = 60
	}
	if rc.AutoStoreIntervalMaxSec < rc.AutoStoreIntervalMinSec {
		rc.AutoStoreIntervalMaxSec = rc.AutoStoreIntervalMinSec + 120
	}
	if rc.AutoStoreDurationSec <= 0 {
		rc.AutoStoreDurationSec = 120
	}
	if rc.AutoStoreDurationSec < 60 {
		rc.AutoStoreDurationSec = 60
	}
	if rc.AutoStoreDurationSec > 86400 {
		rc.AutoStoreDurationSec = 86400
	}
	if rc.AutoStoreTickSec <= 0 {
		rc.AutoStoreTickSec = 10
	}
	if rc.AutoStoreTickSec > 300 {
		rc.AutoStoreTickSec = 300
	}
	if rc.AutoStoreMaxPositionTries <= 0 {
		rc.AutoStoreMaxPositionTries = 10
	}
	if rc.AutoStoreMaxPositionTries > 10000 {
		rc.AutoStoreMaxPositionTries = 10000
	}
	if rc.AutoStoreFailCooldownSec <= 0 {
		rc.AutoStoreFailCooldownSec = 60
	}
	if rc.AutoStoreFailCooldownSec > 3600 {
		rc.AutoStoreFailCooldownSec = 3600
	}
	if rc.SchedulerBadRecoverSec <= 0 {
		rc.SchedulerBadRecoverSec = 60
	}
	if rc.SchedulerBadFailures <= 0 {
		rc.SchedulerBadFailures = 3
	}
	if rc.SchedulerMetricsIntervalSec <= 0 {
		rc.SchedulerMetricsIntervalSec = 10
	}
	if rc.SchedulerMetricsIntervalSec > 300 {
		rc.SchedulerMetricsIntervalSec = 300
	}
	if rc.SchedulerStoreConcurrent <= 0 {
		rc.SchedulerStoreConcurrent = 30
	}
	if rc.SchedulerOnlineBatchSize > 120 {
		rc.SchedulerOnlineBatchSize = 120
	}
	if rc.SchedulerOnlineStartRate <= 0 {
		rc.SchedulerOnlineStartRate = 20
	}
	if rc.SchedulerOnlineStartRate > 60 {
		rc.SchedulerOnlineStartRate = 60
	}
	if rc.SchedulerOnlineFillTimeout <= 0 {
		rc.SchedulerOnlineFillTimeout = 60
	}
	if rc.SchedulerBreakerAbnormalPct <= 0 {
		rc.SchedulerBreakerAbnormalPct = 30
	}
	if rc.SchedulerBreakerAbnormalPct > 100 {
		rc.SchedulerBreakerAbnormalPct = 100
	}
	if rc.SchedulerBreakerPauseSec <= 0 {
		rc.SchedulerBreakerPauseSec = 300
	}
	if rc.SchedulerBreakerPauseSec < 30 {
		rc.SchedulerBreakerPauseSec = 30
	}
	if rc.SchedulerBreakerPauseSec > 3600 {
		rc.SchedulerBreakerPauseSec = 3600
	}
	if rc.SchedulerBreakerReleaseBatch <= 0 {
		rc.SchedulerBreakerReleaseBatch = 20
	}
	if rc.SchedulerBreakerReleaseBatch > 120 {
		rc.SchedulerBreakerReleaseBatch = 120
	}
	if rc.SchedulerBreakerFloorPct < 0 {
		rc.SchedulerBreakerFloorPct = 0
	}
	if rc.SchedulerBreakerFloorPct > 100 {
		rc.SchedulerBreakerFloorPct = 100
	}
	if rc.SchedulerPortDownReleaseBatch <= 0 {
		rc.SchedulerPortDownReleaseBatch = 20
	}
	if rc.SchedulerPortDownReleaseBatch > 120 {
		rc.SchedulerPortDownReleaseBatch = 120
	}
	if rc.SystemActorPollMS <= 0 {
		rc.SystemActorPollMS = 1000
	}
	if rc.SystemActorPollMS < 100 {
		rc.SystemActorPollMS = 100
	}
	if rc.SystemActorPollMS > 10000 {
		rc.SystemActorPollMS = 10000
	}
	if rc.SystemManualActionTimeoutSec <= 0 {
		rc.SystemManualActionTimeoutSec = 60
	}
	if rc.SystemManualActionTimeoutSec > 3600 {
		rc.SystemManualActionTimeoutSec = 3600
	}
	if rc.SystemPacketRatePerSec <= 0 {
		rc.SystemPacketRatePerSec = 20
	}
	if rc.StoreItemSlots <= 0 {
		rc.StoreItemSlots = 1
	}
	if rc.StoreItemSlots > 24 {
		rc.StoreItemSlots = 24
	}
	if rc.StoreItemCountMin <= 0 {
		rc.StoreItemCountMin = 1
	}
	if rc.StoreItemCountMax <= 0 {
		rc.StoreItemCountMax = rc.StoreItemCountMin
	}
	if rc.StoreItemCountMax < rc.StoreItemCountMin {
		rc.StoreItemCountMin, rc.StoreItemCountMax = rc.StoreItemCountMax, rc.StoreItemCountMin
	}
	if rc.StorePriceMin <= 0 {
		rc.StorePriceMin = 100000
	}
	if rc.StorePriceMax < rc.StorePriceMin {
		rc.StorePriceMin, rc.StorePriceMax = rc.StorePriceMax, rc.StorePriceMin
	}
	if rc.StoreInventoryStartBox <= 0 || rc.StoreInventoryStartBox == 105 {
		rc.StoreInventoryStartBox = 7
	}
	if rc.StoreInventoryStartBox > 240 {
		rc.StoreInventoryStartBox = 240
	}
	if rc.StoreConfirmTimeoutSec <= 0 {
		rc.StoreConfirmTimeoutSec = 30
	}
	if rc.StoreConfirmTimeoutSec > 35 {
		rc.StoreConfirmTimeoutSec = 35
	}
	if rc.SpawnVillage < 1 {
		rc.SpawnVillage = 1
	}
	if rc.SpawnVillage > 3 {
		rc.SpawnVillage = 3
	}
}
