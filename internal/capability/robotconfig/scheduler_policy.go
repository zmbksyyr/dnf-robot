package robotconfig

func TargetCapacity(rc RuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	return target
}

func CreateRoom(rc RuntimeConfig, existing int) int {
	room := TargetCapacity(rc) - existing
	if room < 0 {
		return 0
	}
	return room
}

func ScaleUpBatch(rc RuntimeConfig) int {
	batch := rc.SchedulerOnlineBatchSize
	if batch < 0 {
		return 0
	}
	if batch <= 0 {
		batch = 20
	}
	if batch > 120 {
		batch = 120
	}
	return batch
}

func PendingActorLimit(target int, rc RuntimeConfig) int {
	if target <= 0 {
		return 1
	}
	limit := OnlineStartRate(rc) * 8
	if limit < target/10 {
		limit = target / 10
	}
	if limit < 5 {
		limit = 5
	}
	if limit > 120 {
		limit = 120
	}
	return limit
}

func ScaleDownBatch(current, target int) int {
	delta := current - target
	if delta <= 0 {
		return 0
	}
	batch := current / 25
	if current%25 != 0 {
		batch++
	}
	if batch < 5 {
		batch = 5
	}
	if batch > 50 {
		batch = 50
	}
	if batch > delta {
		batch = delta
	}
	return batch
}

func OnlineStartRate(rc RuntimeConfig) int {
	rate := rc.SchedulerOnlineStartRate
	if rate < 0 {
		return 0
	}
	if rate <= 0 {
		rate = 20
	}
	if rate > 60 {
		rate = 60
	}
	return rate
}

func OnlineStartRateForNeed(need int, rc RuntimeConfig) int {
	rate := OnlineStartRate(rc)
	if rate <= 0 {
		return 0
	}
	if need <= 0 {
		return rate
	}
	timeout := rc.SchedulerOnlineFillTimeout
	if timeout <= 0 {
		timeout = 60
	}
	required := (need + timeout - 1) / timeout
	if required > rate {
		rate = required
	}
	if rate > 60 {
		return 60
	}
	return rate
}

func BreakerActorFloor(rc RuntimeConfig) int {
	target := TargetCapacity(rc)
	floorPct := rc.SchedulerBreakerFloorPct
	if floorPct < 0 {
		floorPct = 0
	}
	if floorPct > 100 {
		floorPct = 100
	}
	floor := target * floorPct / 100
	if target <= 50 && floor < target {
		return target
	}
	return floor
}

func NormalizedTarget(rc RuntimeConfig) int {
	target := rc.AutoTargetOnlineCount
	if target < 0 {
		target = 0
	}
	if rc.MaxOnlineRobots > 0 && target > rc.MaxOnlineRobots {
		target = rc.MaxOnlineRobots
	}
	if target <= 0 {
		target = 20
	}
	return target
}

func TargetScale(target int) int {
	scale := target / 100
	if target%100 != 0 {
		scale++
	}
	if scale < 1 {
		scale = 1
	}
	if scale > 10 {
		scale = 10
	}
	return scale
}

func Clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
