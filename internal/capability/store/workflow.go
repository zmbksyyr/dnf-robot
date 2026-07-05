package store

import (
	"fmt"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"time"
)

type Workflow struct {
	Env WorkflowEnv
}

type WorkflowEnv interface {
	AddAutoStore(success, failed, expired int)
	AcquireAutoStoreSlot(rc robotconfig.RuntimeConfig) (chan struct{}, bool)
	BeginStoreBusy(uid int) bool
	CompletePrivateStoreDisplay(uid int) bool
	Config() robotconfig.RuntimeConfig
	EndStoreBusy(uid int)
	EnsureStoreInventoryAndStall(info robotcap.Info, rc robotconfig.RuntimeConfig) error
	FinishStoreState(uid, cid int, reason string)
	Logf(format string, args ...interface{})
	Logout(req robotcap.CommandRequest) (robotcap.CommandResult, error)
	MarkStoreStarted(uid int) error
	Online(req robotcap.CommandRequest, store bool, confirm bool) (robotcap.CommandResult, error)
	PrepareStorePosition(info robotcap.Info) error
	ReleaseAutoStoreSlot(slots chan struct{})
	RestoreAutoNormalPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) robotcap.Info
	RobotGamePort() int
	RuntimeStatusMap() map[int]robotcap.RuntimeStatus
	SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error)
	SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool
	StartPrivateStore(uid int, title string) bool
	StorePoints() *PointCoordinator
	SyncRobotCharacterVillage(cid int, village int) error
}

type AutoAttemptState int

const (
	AutoAttemptFailed AutoAttemptState = iota
	AutoAttemptSuccess
	AutoAttemptBusy
	AutoAttemptCancelled
)

func (w Workflow) Store(req robotcap.CommandRequest) (robotcap.CommandResult, error) {
	env := w.Env
	robots, err := env.SelectRobots(req)
	if err != nil {
		return robotcap.CommandResult{}, err
	}
	rc := env.Config()
	status := env.RuntimeStatusMap()
	result := robotcap.NewCommandResult(len(robots))
	var offline []robotcap.Info
	for _, r := range robots {
		if !env.BeginStoreBusy(r.UID) {
			result.Failed++
			result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateStoreBusy, Message: "store already running for uid"})
			continue
		}
		if err := env.EnsureStoreInventoryAndStall(r, rc); err != nil {
			result.Failed++
			result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateStorePrepareFailed, Message: err.Error()})
			env.EndStoreBusy(r.UID)
			continue
		}
		if st, ok := status[r.UID]; ok && robotcap.ActiveRuntimeStatus(st) {
			logoutResult, err := env.Logout(robotcap.CommandRequest{UIDs: []int{r.UID}})
			if err != nil || logoutResult.Confirmed == 0 {
				msg := fmt.Sprintf("logout before store failed: err=%v confirmed=%d", err, logoutResult.Confirmed)
				env.Logf("[Store] uid=%d %s\n", r.UID, msg)
				result.Failed++
				result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateLogoutFailed, Message: msg})
				env.EndStoreBusy(r.UID)
				continue
			}
			if rc.ReconnectDelayMS > 0 {
				time.Sleep(time.Duration(rc.ReconnectDelayMS) * time.Millisecond)
			}
		}
		offline = append(offline, r)
	}
	if len(offline) > 0 {
		online, err := env.Online(robotcap.CommandRequest{UIDs: robotcap.UIDs(offline)}, false, true)
		if err != nil {
			for _, r := range offline {
				env.EndStoreBusy(r.UID)
			}
			return result, err
		}
		onlineOK := make(map[int]robotcap.ActionResult)
		for _, robot := range online.Robots {
			if robot.OK && robot.State == robotcap.ActionStateRunning {
				onlineOK[robot.UID] = robot
			}
		}
		for _, r := range offline {
			if _, ok := onlineOK[r.UID]; !ok {
				result.Failed++
				result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateNotOnline, Message: "online before store failed"})
				env.FinishStoreState(r.UID, r.CID, "store_online_failed")
				env.EndStoreBusy(r.UID)
				continue
			}
			title := fmt.Sprintf("tw-%d", r.UID%100000)
			if env.StartPrivateStore(r.UID, title) {
				_ = env.MarkStoreStarted(r.UID)
				result.Accepted++
				result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateAccepted})
			} else {
				result.Failed++
				result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateStoreStartFailed, Message: "StartPrivateStore failed after online"})
				env.FinishStoreState(r.UID, r.CID, "store_start_failed")
				env.EndStoreBusy(r.UID)
			}
		}
	}
	deadline := time.Now().Add(time.Duration(rc.StoreConfirmTimeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		status = env.RuntimeStatusMap()
		for i := range result.Robots {
			if result.Robots[i].OK || result.Robots[i].State != robotcap.ActionStateAccepted {
				continue
			}
			st, ok := status[result.Robots[i].UID]
			if !ok || !st.StoreDisplayAck {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	status = env.RuntimeStatusMap()
	for i := range result.Robots {
		if result.Robots[i].OK || result.Robots[i].State != robotcap.ActionStateAccepted {
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && robotcap.ActiveRuntimeStatus(st) && st.StoreDisplayAck {
			result.Robots[i].OK = true
			result.Robots[i].State = robotcap.ActionStateStore
			result.Confirmed++
		} else {
			result.Robots[i].State = robotcap.ActionStateNotConfirmed
			result.Robots[i].Message = "store state not confirmed"
			result.Failed++
			env.FinishStoreState(result.Robots[i].UID, result.Robots[i].CID, "store_not_confirmed")
		}
		env.EndStoreBusy(result.Robots[i].UID)
	}
	return result, nil
}

func (w Workflow) AutoUntilSuccess(st robotcap.RuntimeStatus, rc robotconfig.RuntimeConfig, shouldStop func() bool) AutoAttemptState {
	env := w.Env
	tries := rc.AutoStoreMaxPositionTries
	if tries <= 0 {
		tries = 10
	}
	info := robotcap.Info{UID: st.UID, CID: st.CID, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y, Port: env.RobotGamePort()}
	if robots, err := env.SelectRobots(robotcap.CommandRequest{UIDs: []int{st.UID}}); err == nil && len(robots) > 0 {
		info.CID = robots[0].CID
		info.Port = robots[0].Port
		info.Level = robots[0].Level
		info.Job = robots[0].Job
		info.Grow = robots[0].Grow
	}
	if !env.BeginStoreBusy(info.UID) {
		return AutoAttemptBusy
	}
	slot, ok := env.AcquireAutoStoreSlot(rc)
	if !ok {
		env.EndStoreBusy(info.UID)
		return AutoAttemptBusy
	}
	defer func() {
		env.ReleaseAutoStoreSlot(slot)
		env.EndStoreBusy(info.UID)
	}()
	points := env.StorePoints()
	for try := 1; try <= tries; try++ {
		if shouldStop != nil && shouldStop() {
			env.Logf("[AutoStore] uid=%d cancelled_before_try=%d\n", info.UID, try)
			return AutoAttemptCancelled
		}
		pos, ok := points.Claim(info.UID)
		if !ok {
			env.Logf("[AutoStore] uid=%d no_store_point try=%d/%d\n", info.UID, try, tries)
			break
		}
		info.Village, info.Area, info.X, info.Y = pos.Village, pos.Area, pos.X, pos.Y
		if ok, reason := w.tryPosition(info, rc, try, shouldStop); ok {
			points.Report(info.UID, pos, try, true, "store_ack")
			env.Logf("[StoreSuccessPoint] uid=%d point=%s region=%s village=%d area=%d x=%d y=%d try=%d source=%s\n", info.UID, pos.PointID, pos.Region, info.Village, info.Area, info.X, info.Y, try, pos.Source)
			env.AddAutoStore(1, 0, 0)
			return AutoAttemptSuccess
		} else if reason != "" {
			points.Report(info.UID, pos, try, false, reason)
			continue
		}
		points.Report(info.UID, pos, try, false, "store_failed")
	}
	points.Flush()
	_, _ = env.Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
	env.FinishStoreState(info.UID, info.CID, "store_failed")
	env.Logf("[AutoStore] uid=%d failed_after=%d\n", info.UID, tries)
	env.AddAutoStore(0, 1, 0)
	env.RestoreAutoNormalPosition(info, rc, "store_failed")
	return AutoAttemptFailed
}

func (w Workflow) tryPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, try int, shouldStop func() bool) (bool, string) {
	env := w.Env
	if shouldStop != nil && shouldStop() {
		return false, "cancelled"
	}
	_, _ = env.Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
	logoutDelay := time.Duration(rc.ReconnectDelayMS) * time.Millisecond
	if logoutDelay < 1500*time.Millisecond {
		logoutDelay = 1500 * time.Millisecond
	}
	if SleepWithStop(logoutDelay, shouldStop) {
		return false, "cancelled"
	}
	if shouldStop != nil && shouldStop() {
		return false, "cancelled"
	}
	if err := env.PrepareStorePosition(info); err != nil {
		env.Logf("[AutoStore] uid=%d dummy_update_failed try=%d err=%v\n", info.UID, try, err)
		return false, "prepare_failed"
	}
	if err := env.SyncRobotCharacterVillage(info.CID, info.Village); err != nil {
		env.Logf("[AutoStore] uid=%d charac_village_sync_failed try=%d cid=%d village=%d err=%v\n", info.UID, try, info.CID, info.Village, err)
		return false, "prepare_failed"
	}
	if err := env.EnsureStoreInventoryAndStall(info, rc); err != nil {
		env.Logf("[AutoStore] uid=%d prepare_failed try=%d err=%v\n", info.UID, try, err)
		return false, "prepare_failed"
	}
	if SleepWithStop(800*time.Millisecond, shouldStop) {
		return false, "cancelled"
	}
	online, err := env.Online(robotcap.CommandRequest{UIDs: []int{info.UID}}, false, true)
	if err != nil || online.Confirmed != 1 {
		env.Logf("[AutoStore] uid=%d store_online_failed try=%d confirmed=%d failed=%d err=%v\n", info.UID, try, online.Confirmed, online.Failed, err)
		env.FinishStoreState(info.UID, info.CID, "store_online_failed")
		return false, "online_failed"
	}
	if err := env.SyncRobotCharacterVillage(info.CID, info.Village); err != nil {
		env.Logf("[AutoStore] uid=%d charac_village_resync_failed try=%d cid=%d village=%d err=%v\n", info.UID, try, info.CID, info.Village, err)
		return false, "prepare_failed"
	}
	if shouldStop != nil && shouldStop() {
		return false, "cancelled"
	}
	fromGate := GateAreaForVillage(info.Village)
	if fromGate != info.Area {
		areaSet := env.SetAreaFrom(info.UID, info.Village, info.Area, info.X, info.Y, info.Village, fromGate)
		if !areaSet {
			env.FinishStoreState(info.UID, info.CID, "set_area_failed")
			return false, "set_area_failed"
		}
		if SleepWithStop(1800*time.Millisecond, shouldStop) {
			return false, "cancelled"
		}
	}
	title := fmt.Sprintf("tw-%d", info.UID%100000)
	if !env.StartPrivateStore(info.UID, title) {
		env.Logf("[AutoStore] uid=%d store_start_failed try=%d\n", info.UID, try)
		env.FinishStoreState(info.UID, info.CID, "store_start_failed")
		return false, "store_start_failed"
	}
	if ok, reason := w.waitDisplay(info.UID, rc, shouldStop); ok {
		return true, ""
	} else if reason != "" {
		_, _ = env.Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
		env.FinishStoreState(info.UID, info.CID, reason)
		return false, reason
	}
	_, _ = env.Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
	env.FinishStoreState(info.UID, info.CID, "store_failed")
	return false, "store_failed"
}

func (w Workflow) waitDisplay(uid int, rc robotconfig.RuntimeConfig, shouldStop func() bool) (bool, string) {
	env := w.Env
	var createdAt time.Time
	var lastDisplayAt time.Time
	displayTries := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return false, "cancelled"
		}
		st, ok := env.RuntimeStatusMap()[uid]
		if !ok || st.StateName != "running" || st.DisconnectReason != 0 {
			return false, "runtime_stopped"
		}
		if st.StoreDisplayAck {
			return true, ""
		}
		if st.StoreDisplayRejected {
			return false, StoreErrReason(st.LastStoreError)
		}
		if st.StoreCreateRejected && !st.StoreCreated {
			return false, StoreErrReason(st.LastStoreError)
		}
		if st.StoreCreated && createdAt.IsZero() {
			createdAt = time.Now()
		}
		if !createdAt.IsZero() && time.Since(createdAt) >= 2*time.Second &&
			(lastDisplayAt.IsZero() || time.Since(lastDisplayAt) >= 2*time.Second) && displayTries < 4 {
			lastDisplayAt = time.Now()
			displayTries++
			if env.CompletePrivateStoreDisplay(uid) {
				return true, ""
			}
		}
		if SleepWithStop(200*time.Millisecond, shouldStop) {
			return false, "cancelled"
		}
	}
	return false, "display_wait_failed"
}

func StoreErrReason(err byte) string {
	if err == 0 {
		return "store_failed"
	}
	return fmt.Sprintf("store_err_0x%02x", err)
}

func SleepWithStop(d time.Duration, shouldStop func() bool) bool {
	if d <= 0 {
		return shouldStop != nil && shouldStop()
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return true
		}
		remaining := time.Until(deadline)
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		time.Sleep(remaining)
	}
	return shouldStop != nil && shouldStop()
}
