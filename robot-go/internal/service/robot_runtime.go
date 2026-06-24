package service

import (
	"sync"
	"time"
)

type RobotRuntime struct {
	manager *RobotManager
	locksMu sync.Mutex
	locks   map[int]*runtimeUIDLock
}

type runtimeUIDLock struct {
	mu   sync.Mutex
	refs int
}

func NewRobotRuntime(manager *RobotManager) *RobotRuntime {
	return &RobotRuntime{manager: manager, locks: make(map[int]*runtimeUIDLock)}
}

func (r *RobotRuntime) Config() robotRuntimeConfig {
	return r.manager.loadRobotConfig()
}

func (r *RobotRuntime) Status(uid int) (RuntimeRobotStatus, bool) {
	st, ok := r.manager.runtimeStatusMap()[uid]
	return st, ok
}

func (r *RobotRuntime) IsActive(uid int) bool {
	st, ok := r.Status(uid)
	if !ok {
		return false
	}
	return activeRuntimeStatus(st)
}

func (r *RobotRuntime) OnlineNoConfirm(uid int) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		res, err := r.manager.OnlineNoConfirm(RobotCommandRequest{UIDs: []int{uid}}, false)
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Logout(uid int) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		res, err := r.manager.Logout(RobotCommandRequest{UIDs: []int{uid}})
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Move(uid int) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		res, err := r.manager.Move(RobotCommandRequest{UIDs: []int{uid}})
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Shout(uid int, world bool) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		res, err := r.manager.ShoutOne(RobotCommandRequest{UIDs: []int{uid}}, world)
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Store(uid int) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		res, err := r.manager.Store(RobotCommandRequest{UIDs: []int{uid}})
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) AutoMove(uid int) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != "running" || st.DisconnectReason != 0 {
			return RobotActionResult{UID: uid, OK: false, State: "offline"}
		}
		rc := r.Config()
		maps := r.manager.loadMapCatalog()
		target, hasTarget := r.manager.currentFollowTarget(rc, maps)
		info := RobotInfo{UID: st.UID, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y}
		if hasTarget && target.UID != st.UID {
			r.manager.autoMoveRobot(info, rc, maps, &target)
		} else {
			r.manager.autoMoveRobot(info, rc, maps, nil)
		}
		r.manager.addAutoMove(1, 0)
		return RobotActionResult{UID: uid, CID: st.CID, OK: true, State: "moved"}
	})
}

func (r *RobotRuntime) AutoShout(uid int, world bool, msg string) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != "running" || st.DisconnectReason != 0 {
			r.manager.addAutoShoutChannel(world, 0, 1)
			return RobotActionResult{UID: uid, OK: false, State: "offline"}
		}
		tpl := r.manager.loadShoutTemplates()
		if msg == "" && len(tpl.Messages) > 0 {
			msg = safeRobotShoutMessage(tpl.Messages[0])
		}
		r.manager.autoShoutRobot(uid, tpl, msg, world)
		r.manager.addAutoShoutChannel(world, 1, 0)
		return RobotActionResult{UID: uid, CID: st.CID, OK: true, State: "sent"}
	})
}

func (r *RobotRuntime) AutoStore(uid int, shouldStop func() bool) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != "running" || st.DisconnectReason != 0 {
			return RobotActionResult{UID: uid, OK: false, State: "offline"}
		}
		if shouldStop != nil && shouldStop() {
			return RobotActionResult{UID: uid, CID: st.CID, OK: false, State: "cancelled"}
		}
		switch r.manager.autoStoreUntilSuccess(st, r.Config(), shouldStop) {
		case autoStoreAttemptSuccess:
			return RobotActionResult{UID: uid, CID: st.CID, OK: true, State: "store"}
		case autoStoreAttemptBusy:
			return RobotActionResult{UID: uid, CID: st.CID, OK: false, State: "store_busy"}
		case autoStoreAttemptCancelled:
			return RobotActionResult{UID: uid, CID: st.CID, OK: false, State: "cancelled"}
		}
		if shouldStop != nil && shouldStop() {
			return RobotActionResult{UID: uid, CID: st.CID, OK: false, State: "cancelled"}
		}
		return RobotActionResult{UID: uid, CID: st.CID, OK: false, State: "store_failed"}
	})
}

func (r *RobotRuntime) ExpireStore(uid int) RobotActionResult {
	return r.run(uid, func() RobotActionResult {
		st, ok := r.Status(uid)
		if !ok {
			return RobotActionResult{UID: uid, OK: true, State: "offline"}
		}
		rc := r.Config()
		_, _ = r.manager.Logout(RobotCommandRequest{UIDs: []int{uid}})
		r.manager.finishStoreState(uid, st.CID, "store_expired")
		r.manager.addAutoStore(0, 0, 1)
		info := RobotInfo{UID: uid, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y, Port: r.manager.cfg.RobotGamePort}
		if robots, err := r.manager.selectRobots(RobotCommandRequest{UIDs: []int{uid}}); err == nil && len(robots) > 0 {
			info = robots[0]
		}
		r.manager.restoreAutoNormalPosition(info, rc, "store_expired")
		return RobotActionResult{UID: uid, CID: st.CID, OK: true, State: "store_expired"}
	})
}

func (r *RobotRuntime) run(uid int, fn func() RobotActionResult) RobotActionResult {
	lock := r.acquireUIDLock(uid)
	lock.mu.Lock()
	defer r.releaseUIDLock(uid, lock)
	defer func() {
		if rec := recover(); rec != nil {
			robotLogf("[RobotRuntime] panic uid=%d err=%v\n", uid, rec)
		}
	}()
	return fn()
}

func (r *RobotRuntime) acquireUIDLock(uid int) *runtimeUIDLock {
	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	if r.locks == nil {
		r.locks = make(map[int]*runtimeUIDLock)
	}
	lock := r.locks[uid]
	if lock == nil {
		lock = &runtimeUIDLock{}
		r.locks[uid] = lock
	}
	lock.refs++
	return lock
}

func (r *RobotRuntime) releaseUIDLock(uid int, lock *runtimeUIDLock) {
	lock.mu.Unlock()
	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	lock.refs--
	if lock.refs <= 0 && r.locks[uid] == lock {
		delete(r.locks, uid)
	}
}

func firstActionResult(uid int, res RobotCommandResult, err error) RobotActionResult {
	if err != nil {
		return RobotActionResult{UID: uid, OK: false, State: "failed", Message: err.Error()}
	}
	for _, robot := range res.Robots {
		if robot.UID == uid {
			return robot
		}
	}
	return RobotActionResult{UID: uid, OK: false, State: "missing", Message: "no action result"}
}

func randomizedDue(next *time.Time, now time.Time, minSec, maxSec int, randBetween func(int, int) int) bool {
	if minSec <= 0 || maxSec <= 0 {
		return false
	}
	if maxSec < minSec {
		minSec, maxSec = maxSec, minSec
	}
	if next.IsZero() {
		*next = now.Add(time.Duration(randBetween(minSec, maxSec)) * time.Second)
		return false
	}
	if now.Before(*next) {
		return false
	}
	*next = now.Add(time.Duration(randBetween(minSec, maxSec)) * time.Second)
	return true
}
