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
	AcquireAutoStoreSlot(rc robotconfig.RuntimeConfig) (func(), bool)
	BeginStoreBusy(uid int) bool
	Config() robotconfig.RuntimeConfig
	EndStoreBusy(uid int)
	EnsureStoreInventoryAndStall(info robotcap.Info, rc robotconfig.RuntimeConfig) error
	FinishStoreState(uid, cid int, reason string)
	Logf(format string, args ...interface{})
	Logout(req robotcap.CommandRequest) (robotcap.CommandResult, error)
	MarkStoreStarted(uid int) error
	Online(req robotcap.CommandRequest, confirm bool) (robotcap.CommandResult, error)
	PrepareStorePosition(info robotcap.Info) error
	RestoreAutoNormalOnline(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) (robotcap.Info, bool)
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
		// Prepare the offline inventory only after logout.  Writing Robot_stall and
		// the character inventory while the game still has an active session lets
		// the server's final online snapshot overwrite the prepared rows, which
		// manifests as store_not_confirmed or store_err_0x11.
		if err := env.EnsureStoreInventoryAndStall(r, rc); err != nil {
			result.Failed++
			result.Robots = append(result.Robots, robotcap.ActionResult{UID: r.UID, CID: r.CID, OK: false, State: robotcap.ActionStateStorePrepareFailed, Message: err.Error()})
			env.EndStoreBusy(r.UID)
			continue
		}
		offline = append(offline, r)
	}
	if len(offline) > 0 {
		online, err := env.Online(robotcap.CommandRequest{UIDs: robotcap.UIDs(offline)}, true)
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
				env.FinishStoreState(r.UID, r.CID, StoreReasonOnlineFailed)
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
				env.FinishStoreState(r.UID, r.CID, StoreReasonStartFailed)
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
			env.FinishStoreState(result.Robots[i].UID, result.Robots[i].CID, StoreReasonNotConfirmed)
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
	releaseSlot, ok := env.AcquireAutoStoreSlot(rc)
	if !ok {
		env.EndStoreBusy(info.UID)
		return AutoAttemptBusy
	}
	defer func() {
		releaseSlot()
		env.EndStoreBusy(info.UID)
	}()
	points := env.StorePoints()
	finalReason := StoreReasonFailed
	attempts := 0
	for try := 1; try <= tries; try++ {
		attempts = try
		if shouldStop != nil && shouldStop() {
			env.Logf("[AutoStore] uid=%d cancelled_before_try=%d\n", info.UID, try)
			return AutoAttemptCancelled
		}
		pos, ok := points.Claim(info.UID)
		if !ok {
			env.Logf("[AutoStore] uid=%d no_store_point try=%d/%d\n", info.UID, try, tries)
			points.Flush()
			return AutoAttemptBusy
		}
		info.Village, info.Area, info.X, info.Y = pos.Village, pos.Area, pos.X, pos.Y
		if ok, reason := w.tryPosition(info, rc, try, shouldStop); ok {
			points.Report(info.UID, pos, try, true, StoreReasonAck)
			env.Logf("[StoreSuccessPoint] uid=%d point=%s region=%s village=%d area=%d x=%d y=%d try=%d source=%s\n", info.UID, pos.PointID, pos.Region, info.Village, info.Area, info.X, info.Y, try, pos.Source)
			env.AddAutoStore(1, 0, 0)
			return AutoAttemptSuccess
		} else if reason != "" {
			if reason == StoreReasonCancelled {
				points.Release(info.UID, pos)
				points.Flush()
				env.FinishStoreState(info.UID, info.CID, reason)
				return AutoAttemptCancelled
			}
			finalReason = reason
			points.Report(info.UID, pos, try, false, reason)
			if !RetryStoreReasonWithNewPoint(reason) {
				finalReason = reason
				env.Logf("[AutoStore] uid=%d hard_fail reason=%s try=%d/%d\n", info.UID, reason, try, tries)
				break
			}
			continue
		}
		points.Report(info.UID, pos, try, false, StoreReasonFailed)
	}
	points.Flush()
	_, _ = env.Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
	env.FinishStoreState(info.UID, info.CID, finalReason)
	env.Logf("[AutoStore] uid=%d failed_after=%d reason=%s\n", info.UID, attempts, finalReason)
	env.AddAutoStore(0, 1, 0)
	if _, recovered := env.RestoreAutoNormalOnline(info, rc, finalReason); !recovered {
		env.Logf("[AutoStore] uid=%d restore_normal_online_failed reason=%s\n", info.UID, finalReason)
	}
	return AutoAttemptFailed
}

func (w Workflow) tryPosition(info robotcap.Info, rc robotconfig.RuntimeConfig, try int, shouldStop func() bool) (bool, string) {
	env := w.Env
	if shouldStop != nil && shouldStop() {
		return false, StoreReasonCancelled
	}
	_, _ = env.Logout(robotcap.CommandRequest{UIDs: []int{info.UID}})
	logoutDelay := time.Duration(rc.ReconnectDelayMS) * time.Millisecond
	if logoutDelay < 1500*time.Millisecond {
		logoutDelay = 1500 * time.Millisecond
	}
	if SleepWithStop(logoutDelay, shouldStop) {
		return false, StoreReasonCancelled
	}
	if shouldStop != nil && shouldStop() {
		return false, StoreReasonCancelled
	}
	if err := env.PrepareStorePosition(info); err != nil {
		env.Logf("[AutoStore] uid=%d dummy_update_failed try=%d err=%v\n", info.UID, try, err)
		return false, StoreReasonPrepareFailed
	}
	if err := env.SyncRobotCharacterVillage(info.CID, info.Village); err != nil {
		env.Logf("[AutoStore] uid=%d charac_village_sync_failed try=%d cid=%d village=%d err=%v\n", info.UID, try, info.CID, info.Village, err)
		return false, StoreReasonPrepareFailed
	}
	if err := env.EnsureStoreInventoryAndStall(info, rc); err != nil {
		env.Logf("[AutoStore] uid=%d prepare_failed try=%d err=%v\n", info.UID, try, err)
		return false, StoreReasonPrepareFailed
	}
	if SleepWithStop(800*time.Millisecond, shouldStop) {
		return false, StoreReasonCancelled
	}
	online, err := env.Online(robotcap.CommandRequest{UIDs: []int{info.UID}}, true)
	if err != nil || online.Confirmed != 1 {
		env.Logf("[AutoStore] uid=%d store_online_failed try=%d confirmed=%d failed=%d err=%v\n", info.UID, try, online.Confirmed, online.Failed, err)
		env.FinishStoreState(info.UID, info.CID, StoreReasonOnlineFailed)
		return false, StoreReasonOnlineAttemptFailed
	}
	if err := env.SyncRobotCharacterVillage(info.CID, info.Village); err != nil {
		env.Logf("[AutoStore] uid=%d charac_village_resync_failed try=%d cid=%d village=%d err=%v\n", info.UID, try, info.CID, info.Village, err)
		return false, StoreReasonPrepareFailed
	}
	if shouldStop != nil && shouldStop() {
		return false, StoreReasonCancelled
	}
	stAfterOnline, stOK := env.RuntimeStatusMap()[info.UID]
	if stOK && stAfterOnline.Village == info.Village && stAfterOnline.Area == info.Area {
		return w.startAndWaitDisplay(info, rc, try, shouldStop)
	}
	if stOK && (stAfterOnline.Village != info.Village || stAfterOnline.Area != info.Area) {
		if !IsAreaEligible(stAfterOnline.Village, stAfterOnline.Area) || !IsAreaEligible(info.Village, info.Area) {
			env.Logf("[AutoStore] uid=%d set_area_skipped_unsafe try=%d from=%d/%d to=%d/%d\n", info.UID, try, stAfterOnline.Village, stAfterOnline.Area, info.Village, info.Area)
			env.FinishStoreState(info.UID, info.CID, StoreReasonSetAreaFailed)
			return false, StoreReasonSetAreaFailed
		}
		areaSet := env.SetAreaFrom(info.UID, info.Village, info.Area, info.X, info.Y, stAfterOnline.Village, stAfterOnline.Area)
		if !areaSet {
			env.FinishStoreState(info.UID, info.CID, StoreReasonSetAreaFailed)
			return false, StoreReasonSetAreaFailed
		}
		if SleepWithStop(1800*time.Millisecond, shouldStop) {
			return false, StoreReasonCancelled
		}
	}
	return w.startAndWaitDisplay(info, rc, try, shouldStop)
}

func (w Workflow) startAndWaitDisplay(info robotcap.Info, rc robotconfig.RuntimeConfig, try int, shouldStop func() bool) (bool, string) {
	env := w.Env
	title := fmt.Sprintf("tw-%d", info.UID%100000)
	if !env.StartPrivateStore(info.UID, title) {
		env.Logf("[AutoStore] uid=%d store_start_failed try=%d\n", info.UID, try)
		env.FinishStoreState(info.UID, info.CID, StoreReasonStartFailed)
		return false, StoreReasonStartFailed
	}
	if ok, reason := w.waitDisplay(info.UID, rc, shouldStop); ok {
		return true, ""
	} else if reason != "" {
		return false, reason
	}
	return false, StoreReasonFailed
}

func (w Workflow) waitDisplay(uid int, rc robotconfig.RuntimeConfig, shouldStop func() bool) (bool, string) {
	env := w.Env
	timeout := time.Duration(rc.StoreConfirmTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if shouldStop != nil && shouldStop() {
			return false, StoreReasonCancelled
		}
		st, ok := env.RuntimeStatusMap()[uid]
		if !ok || st.StateName != robotcap.RuntimeStateRunning || st.DisconnectReason != 0 {
			return false, StoreReasonRuntimeStopped
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
		if SleepWithStop(200*time.Millisecond, shouldStop) {
			return false, StoreReasonCancelled
		}
	}
	return false, StoreReasonDisplayWaitFailed
}

func StoreErrReason(err byte) string {
	// df_game_r CMD 88 (CreatePrivateStore) error classification, verified from
	// server-side branches rather than inferred from robot success rates:
	//   0x38: village object registration failed. Usually a point collision or an
	//         invalid object position, so changing coordinates is appropriate.
	//   0x3e: generic "store creation is not allowed here/now". The server reuses
	//         it for busy state, forbidden village/channel, gate/entrance area and
	//         village/area mismatch; only some branches are position-related.
	//   0x52: coordinates are inside a configured restrictive commercial zone;
	//         this is the definite position error (often NPC/entrance space).
	//   0x72: account/trading security protection rejected the operation; it is
	//         not a map-position error and changing coordinates cannot fix it.
	//   0x11: store item/inventory verification failed after creation; it is not
	//         a map-position error.
	if err == 0 {
		return StoreReasonFailed
	}
	if err == 0x11 {
		return StoreReasonErr011
	}
	return fmt.Sprintf("store_err_0x%02x", err)
}

func RetryStoreReasonWithNewPoint(reason string) bool {
	// 0x38 and 0x52 are position failures. 0x3e remains retryable because the
	// server also uses it for gate/entrance/area restrictions, although it is not
	// exclusively positional. 0x11 and 0x72 must not be treated as "try a point".
	switch reason {
	case "store_err_0x38", "store_err_0x3e", StoreReasonErr052:
		return true
	default:
		return false
	}
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
