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
	"time"
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
	return r.manager.runtimeStatus(uid)
}

func (r *RobotRuntime) PartyActive(uid int) bool {
	return r.manager.doll.PartyActive(uid)
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

func (r *RobotRuntime) ForceClose(uid int) bool {
	closer, ok := r.manager.doll.(interface{ ForceClose(int) bool })
	return ok && closer.ForceClose(uid)
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
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 || st.PartyActive || r.PartyActive(uid) {
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOffline}
		}
		rc := r.Config()
		maps := r.manager.loadMapCatalog()
		target, hasTarget := r.manager.currentFollowTarget(rc, maps)
		info := robotcap.Info{UID: st.UID, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y}
		var err error
		if hasTarget && target.UID != st.UID {
			err = r.manager.moveService().AutoMove(info, rc, maps, &target)
		} else {
			err = r.manager.moveService().AutoMove(info, rc, maps, nil)
		}
		if err != nil {
			r.manager.addAutoMove(0, 1)
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateFailed, Message: err.Error()}
		}
		r.manager.addAutoMove(1, 0)
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: true, State: robotcap.ActionStateMoved}
	})
}

func (r *RobotRuntime) AutoShout(uid int, world bool, msg string) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 || st.PartyActive || r.PartyActive(uid) {
			r.manager.addAutoShoutChannel(world, 0, 1)
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOffline}
		}
		tpl := r.manager.loadShoutTemplates()
		if msg == "" && len(tpl.Messages) > 0 {
			msg = robottemplate.SafeShoutMessage(tpl.Messages[0])
		}
		if err := r.manager.shoutService().AutoShout(uid, msg, world); err != nil {
			r.manager.addAutoShoutChannel(world, 0, 1)
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateFailed, Message: err.Error()}
		}
		r.manager.addAutoShoutChannel(world, 1, 0)
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: true, State: robotcap.ActionStateSent}
	})
}

func (r *RobotRuntime) AutoStore(uid int, shouldStop func() bool) robotcap.ActionResult {
	return r.run(uid, func() robotcap.ActionResult {
		st, ok := r.Status(uid)
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 || st.PartyActive || r.PartyActive(uid) {
			return robotcap.ActionResult{UID: uid, OK: false, State: robotcap.ActionStateOffline}
		}
		if shouldStop != nil && shouldStop() {
			return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateCancelled}
		}
		disjoint, releaseStoreType := r.manager.beginAdaptiveStoreType()
		defer releaseStoreType()
		if disjoint {
			return r.autoDisjointStore(uid, st, shouldStop)
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

const disjointStoreCostGold = 500

func (r *RobotRuntime) autoDisjointStore(uid int, st robotcap.RuntimeStatus, shouldStop func() bool) robotcap.ActionResult {
	rc := r.Config()
	info := robotcap.Info{UID: uid, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y, Port: r.manager.cfg.RobotGamePort}
	if robots, err := r.manager.repo().SelectRobots(robotcap.CommandRequest{UIDs: []int{uid}}); err == nil && len(robots) > 0 {
		info = robots[0]
	}
	if !r.manager.beginStoreBusy(uid) {
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateStoreBusy}
	}
	slot, ok := r.manager.acquireAutoStoreSlot(rc)
	if !ok {
		r.manager.endStoreBusy(uid)
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: false, State: robotcap.ActionStateStoreBusy}
	}
	defer func() {
		r.manager.releaseAutoStoreSlot(slot)
		r.manager.endStoreBusy(uid)
	}()

	points := r.manager.storePoints()
	tries := rc.AutoStoreMaxPositionTries
	if tries <= 0 {
		tries = 10
	}
	for try := 1; try <= tries; try++ {
		if shouldStop != nil && shouldStop() {
			return robotcap.ActionResult{UID: uid, CID: info.CID, OK: false, State: robotcap.ActionStateCancelled}
		}
		pos, ok := points.Claim(uid)
		if !ok {
			break
		}
		info.Village, info.Area, info.X, info.Y = pos.Village, pos.Area, pos.X, pos.Y
		ok, reason := r.tryDisjointPosition(info, rc, shouldStop)
		if ok {
			points.Report(uid, pos, try, true, "disjoint_ack")
			r.manager.addAutoStore(1, 0, 0)
			robotLogf("[DISJOINT_SUCCESS_POINT] uid=%d point=%s village=%d area=%d x=%d y=%d try=%d\n", uid, pos.PointID, pos.Village, pos.Area, pos.X, pos.Y, try)
			return robotcap.ActionResult{UID: uid, CID: info.CID, OK: true, State: robotcap.ActionStateStore}
		}
		if reason == "cancelled" {
			points.Release(uid, pos)
			points.Flush()
			r.manager.finishStoreState(uid, info.CID, reason)
			r.manager.doll.ResetDisjointStore(uid)
			return robotcap.ActionResult{UID: uid, CID: info.CID, OK: false, State: robotcap.ActionStateCancelled}
		}
		if reason == "" {
			reason = "disjoint_failed"
		}
		points.Report(uid, pos, try, false, reason)
	}
	points.Flush()
	_, _ = r.manager.sessionService().Logout(robotcap.CommandRequest{UIDs: []int{uid}})
	r.manager.finishStoreState(uid, info.CID, "disjoint_failed")
	r.manager.addAutoStore(0, 1, 0)
	_, _ = r.manager.restoreAutoNormalOnline(info, rc, "disjoint_failed")
	return robotcap.ActionResult{UID: uid, CID: info.CID, OK: false, State: robotcap.ActionStateStoreFailed}
}

func (r *RobotRuntime) tryDisjointPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, shouldStop func() bool) (bool, string) {
	if shouldStop != nil && shouldStop() {
		return false, "cancelled"
	}
	_, _ = r.manager.sessionService().Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
	logoutDelay := time.Duration(rc.ReconnectDelayMS) * time.Millisecond
	if logoutDelay < 15*time.Second {
		logoutDelay = 15 * time.Second
	}
	if sleepWithStop(logoutDelay, shouldStop) {
		return false, "cancelled"
	}
	if err := r.manager.schemaRepo().EnsureDisjointProfession(info); err != nil {
		robotLogf("[DISJOINT_PROFESSION_ERROR] uid=%d cid=%d err=%v\n", info.UID, info.CID, err)
		return false, "profession_failed"
	}
	if sleepWithStop(2*time.Second, shouldStop) {
		return false, "cancelled"
	}
	if err := r.manager.schemaRepo().EnsureDisjointProfession(info); err != nil {
		robotLogf("[DISJOINT_PROFESSION_VERIFY_ERROR] uid=%d cid=%d err=%v\n", info.UID, info.CID, err)
		return false, "profession_verify_failed"
	}
	if err := r.manager.schemaRepo().PrepareDisjointPosition(info, disjointStoreCostGold); err != nil {
		robotLogf("[DISJOINT_POSITION_ERROR] uid=%d err=%v\n", info.UID, err)
		return false, "prepare_failed"
	}
	if _, err := r.manager.schemaRepo().SyncCharacterVillage(info.CID, info.Village); err != nil {
		robotLogf("[DISJOINT_VILLAGE_ERROR] uid=%d cid=%d village=%d err=%v\n", info.UID, info.CID, info.Village, err)
		return false, "prepare_failed"
	}
	// Disjoint stalls are created by CMD 238 after the robot is fully running.
	// Keep the login path normal here; setting disopen/discost in Dummylist and
	// then also sending CMD 238 can leave the runtime without a direct 238 ACK.
	online, err := r.manager.sessionService().Online(robotcap.CommandRequest{UIDs: []int{info.UID}}, false, true, rc)
	if err != nil || online.Confirmed != 1 {
		robotLogf("[DISJOINT_ONLINE_ERROR] uid=%d confirmed=%d failed=%d err=%v\n", info.UID, online.Confirmed, online.Failed, err)
		return false, "online_failed"
	}
	if sleepWithStop(time.Second, shouldStop) {
		return false, "cancelled"
	}
	fromGate := storecap.GateAreaForVillage(info.Village)
	if fromGate != info.Area {
		if !r.manager.doll.SetAreaFrom(info.UID, info.Village, info.Area, info.X, info.Y, info.Village, fromGate) {
			return false, "set_area_failed"
		}
		if sleepWithStop(1800*time.Millisecond, shouldStop) {
			return false, "cancelled"
		}
	}
	if !r.manager.doll.StartDisjointStore(info.UID, disjointStoreCostGold) {
		return false, "start_failed"
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return false, "cancelled"
		}
		if st, ok := r.manager.runtimeStatus(info.UID); ok {
			if st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 {
				return false, "runtime_stopped"
			}
			if st.RobotType == 3 && st.DisjointActive {
				return true, ""
			}
			if st.LastDisjointError != 0 {
				return false, "disjoint_err"
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false, "ack_timeout"
}

func sleepWithStop(d time.Duration, shouldStop func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return true
		}
		step := time.Until(deadline)
		if step > 100*time.Millisecond {
			step = 100 * time.Millisecond
		}
		time.Sleep(step)
	}
	return shouldStop != nil && shouldStop()
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
		_, recovered := r.manager.restoreAutoNormalOnline(info, rc, "store_expired")
		return robotcap.ActionResult{UID: uid, CID: st.CID, OK: recovered, State: robotcap.ActionStateStoreExpired}
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

	lookup, ok := m.loadFollowAccount(account)
	if !ok {
		return robotaction.FollowTarget{}, false
	}
	if len(lookup.uids) > 0 {
		status := m.runtimeStatusMap()
		for _, uid := range lookup.uids {
			if st, ok := status[uid]; ok && robotcap.ActiveRuntimeStatus(st) {
				return robotaction.FollowTarget{UID: uid, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y}, true
			}
		}
	}

	if !lookup.villageOK {
		return robotaction.FollowTarget{}, false
	}
	info := robotcap.Info{Village: lookup.village, Area: rc.SpawnArea, X: m.randBetween(rc.SpawnXMin, rc.SpawnXMax), Y: m.randBetween(rc.SpawnYMin, rc.SpawnYMax), Level: rc.LevelMax}
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
