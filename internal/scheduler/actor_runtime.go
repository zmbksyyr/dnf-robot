package scheduler

import (
	actormodel "robot/internal/actor"
	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/capability/robotspawn"
	robottemplate "robot/internal/capability/robottemplate"
	storecap "robot/internal/capability/store"
	"robot/internal/foundation/lockhub"
	"robot/internal/shared"
	"strings"
)

type RobotRuntime struct {
	manager  *RobotManager
	uidLocks *lockhub.RefHub
}

var _ actormodel.RobotRuntime = (*RobotRuntime)(nil)

func NewRobotRuntime(manager *RobotManager) *RobotRuntime {
	return &RobotRuntime{manager: manager, uidLocks: lockhub.NewRefHub()}
}

func (r *RobotRuntime) Config() robotconfig.RuntimeConfig {
	return r.manager.loadRobotConfig()
}

func (r *RobotRuntime) Status(uid int) (robotcap.RuntimeStatus, bool) {
	st, ok := r.manager.runtimeStatusMap()[uid]
	return st, ok
}

func (r *RobotRuntime) IsActive(uid int) bool {
	st, ok := r.Status(uid)
	if !ok {
		return false
	}
	return robotcap.ActiveRuntimeStatus(st)
}

func (r *RobotRuntime) FinishStoreState(uid, cid int, reason string) {
	r.manager.finishStoreState(uid, cid, reason)
}

func (r *RobotRuntime) AddAutoOnline(success, failed int) {
	r.manager.addAutoOnline(success, failed)
}

func (r *RobotRuntime) AutoActionsEnabled(rc robotconfig.RuntimeConfig) bool {
	return r.manager.autoActionsEnabled(rc)
}

func (r *RobotRuntime) RandomShoutMessage(randIntn func(int) int) string {
	tpl := r.manager.loadShoutTemplates()
	if len(tpl.Messages) == 0 {
		return ""
	}
	idx := 0
	if randIntn != nil {
		idx = randIntn(len(tpl.Messages))
	}
	return robottemplate.SafeShoutMessage(tpl.Messages[idx])
}

func (r *RobotRuntime) OnlineNoConfirm(uid int) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		res, err := r.manager.sessionService().Online(robotcap.CommandRequest{UIDs: []int{uid}}, false, false, r.Config())
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Logout(uid int) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		res, err := r.manager.sessionService().Logout(robotcap.CommandRequest{UIDs: []int{uid}})
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Move(uid int) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		res, err := r.manager.moveService().Move(robotcap.CommandRequest{UIDs: []int{uid}}, r.Config())
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Shout(uid int, world bool) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		res, err := r.manager.shoutService().ShoutOne(robotcap.CommandRequest{UIDs: []int{uid}}, world)
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) Store(uid int) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		res, err := r.manager.storeWorkflow().Store(robotcap.CommandRequest{UIDs: []int{uid}})
		return firstActionResult(uid, res, err)
	})
}

func (r *RobotRuntime) AutoMove(uid int) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 {
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOffline}
		}
		rc := r.Config()
		maps := r.manager.loadMapCatalog()
		target, hasTarget := r.manager.currentFollowTarget(rc, maps)
		info := robotcap.Info{UID: st.UID, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y}
		if hasTarget && target.UID != st.UID {
			r.manager.moveService().AutoMove(info, rc, maps, &target)
		} else {
			r.manager.moveService().AutoMove(info, rc, maps, nil)
		}
		r.manager.addAutoMove(1, 0)
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: true, State: robotcap.ActionStateMoved}
	})
}

func (r *RobotRuntime) AutoShout(uid int, world bool, msg string) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 {
			r.manager.addAutoShoutChannel(world, 0, 1)
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOffline}
		}
		tpl := r.manager.loadShoutTemplates()
		if msg == "" && len(tpl.Messages) > 0 {
			msg = robottemplate.SafeShoutMessage(tpl.Messages[0])
		}
		r.manager.shoutService().AutoShout(uid, msg, world)
		r.manager.addAutoShoutChannel(world, 1, 0)
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: true, State: robotcap.ActionStateSent}
	})
}

func (r *RobotRuntime) AutoStore(uid int, shouldStop func() bool) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 {
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOffline}
		}
		if shouldStop != nil && shouldStop() {
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateCancelled}
		}
		switch r.manager.storeWorkflow().AutoUntilSuccess(st, r.Config(), shouldStop) {
		case storecap.AutoAttemptSuccess:
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: true, State: robotcap.ActionStateStore}
		case storecap.AutoAttemptBusy:
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateStoreBusy}
		case storecap.AutoAttemptCancelled:
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateCancelled}
		}
		if shouldStop != nil && shouldStop() {
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateCancelled}
		}
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateStoreFailed}
	})
}

func (r *RobotRuntime) ExpireStore(uid int) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		st, ok := r.Status(uid)
		if !ok {
			return robotcap.ActionResult{UID: uid, OK: true, State: robotcap.ActionStateOffline}
		}
		rc := r.Config()
		_, _ = r.manager.sessionService().Logout(robotcap.CommandRequest{UIDs: []int{uid}})
		r.manager.finishStoreState(uid, st.CID, "store_expired")
		r.manager.addAutoStore(0, 0, 1)
		info := robotcap.Info{UID: uid, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y, Port: r.manager.cfg.RobotGamePort}
		if robots, err := r.manager.repo().SelectRobots(robotcap.CommandRequest{UIDs: []int{uid}}); err == nil && len(robots) > 0 {
			info = robots[0]
		}
		r.manager.restoreAutoNormalPosition(info, rc, "store_expired")
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: true, State: robotcap.ActionStateStoreExpired}
	})
}

func (r *RobotRuntime) run(uid int, fn func() robotcap.ActionResult) robotcap.ActionResult {
	lock := r.uidLocks.Acquire(uid)
	defer r.uidLocks.Release(uid, lock)
	defer func() {
		if rec := recover(); rec != nil {
			robotLogf("[RobotRuntime] panic uid=%d err=%v\n", uid, rec)
		}
	}()
	return fn()
}

func (r *RobotRuntime) uidLockActive(uid int) bool {
	return r.uidLocks.Active(uid)
}

func (m *RobotManager) currentFollowTarget(rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) (robotaction.FollowTarget, bool) {
	account := strings.TrimSpace(rc.FollowAccount)
	if account == "" || rc.SpawnFixed {
		return robotaction.FollowTarget{}, false
	}

	uids, err := m.schemaRepo().FollowAccountUIDs(account)
	if err == nil {
		if len(uids) > 0 {
			status := m.runtimeStatusMap()
			for _, uid := range uids {
				if st, ok := status[uid]; ok && robotcap.ActiveRuntimeStatus(st) {
					return robotaction.FollowTarget{UID: uid, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y}, true
				}
			}
		}
	}

	village, ok, err := m.schemaRepo().FollowAccountVillageLastPlayed(account)
	if err != nil || !ok {
		return robotaction.FollowTarget{}, false
	}
	info := robotcap.Info{Village: village, Area: rc.SpawnArea, X: m.randBetween(rc.SpawnXMin, rc.SpawnXMax), Y: m.randBetween(rc.SpawnYMin, rc.SpawnYMax), Level: rc.LevelMax}
	robotspawn.ApplyVillageLocation(spawnEnv{manager: m}, &info, info.Village, rc, maps)
	return robotaction.FollowTarget{Village: info.Village, Area: info.Area, X: info.X, Y: info.Y}, true
}

func firstActionResult(uid int, res robotcap.CommandResult, err error) robotcap.ActionResult {
	if err != nil {
		return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateFailed, Message: err.Error()}
	}
	for _, robot := range res.Robots {
		if robot.UID == uid {
			return robot
		}
	}
	return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateMissing, Message: "no action result"}
}
