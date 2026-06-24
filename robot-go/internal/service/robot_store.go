package service

import (
	"fmt"
	"time"
)

func (m *RobotManager) Store(req RobotCommandRequest) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	rc := m.loadRobotConfig()
	status := m.runtimeStatusMap()
	result := newCommandResult(len(robots))
	var offline []RobotInfo
	for _, r := range robots {
		if !m.beginStoreBusy(r.UID) {
			result.Failed++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "store_busy", Message: "store already running for uid"})
			continue
		}
		if err := m.ensureStoreInventoryAndStall(r, rc); err != nil {
			result.Failed++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "store_prepare_failed", Message: err.Error()})
			m.endStoreBusy(r.UID)
			continue
		}
		if st, ok := status[r.UID]; ok && activeRuntimeStatus(st) {
			logoutResult, err := m.Logout(RobotCommandRequest{UIDs: []int{r.UID}})
			if err != nil || logoutResult.Confirmed == 0 {
				msg := fmt.Sprintf("logout before store failed: err=%v confirmed=%d", err, logoutResult.Confirmed)
				robotLogf("[Store] uid=%d %s\n", r.UID, msg)
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "logout_failed", Message: msg})
				m.endStoreBusy(r.UID)
				continue
			}
			if rc.ReconnectDelayMS > 0 {
				time.Sleep(time.Duration(rc.ReconnectDelayMS) * time.Millisecond)
			}
		}
		offline = append(offline, r)
	}
	if len(offline) > 0 {
		online, err := m.Online(RobotCommandRequest{UIDs: robotUIDs(offline)}, false)
		if err != nil {
			for _, r := range offline {
				m.endStoreBusy(r.UID)
			}
			return result, err
		}
		onlineOK := make(map[int]RobotActionResult)
		for _, robot := range online.Robots {
			if robot.OK && robot.State == "running" {
				onlineOK[robot.UID] = robot
			}
		}
		for _, r := range offline {
			if _, ok := onlineOK[r.UID]; !ok {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "not_online", Message: "online before store failed"})
				m.finishStoreState(r.UID, r.CID, "store_online_failed")
				m.endStoreBusy(r.UID)
				continue
			}
			title := fmt.Sprintf("tw-%d", r.UID%100000)
			if m.doll.StartPrivateStore(r.UID, title) {
				_, _ = m.db.Exec("UPDATE d_starsky.Dummylist SET function_type=2 WHERE UID=?", r.UID)
				result.Accepted++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted"})
			} else {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "store_start_failed", Message: "StartPrivateStore failed after online"})
				m.finishStoreState(r.UID, r.CID, "store_start_failed")
				m.endStoreBusy(r.UID)
			}
		}
	}
	deadline := time.Now().Add(time.Duration(rc.StoreConfirmTimeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		status = m.runtimeStatusMap()
		for i := range result.Robots {
			if result.Robots[i].OK || result.Robots[i].State != "accepted" {
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
	status = m.runtimeStatusMap()
	for i := range result.Robots {
		if result.Robots[i].OK || result.Robots[i].State != "accepted" {
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) && st.StoreDisplayAck {
			result.Robots[i].OK = true
			result.Robots[i].State = "store"
			result.Confirmed++
		} else {
			result.Robots[i].State = "not_confirmed"
			result.Robots[i].Message = "store state not confirmed"
			result.Failed++
			m.finishStoreState(result.Robots[i].UID, result.Robots[i].CID, "store_not_confirmed")
		}
		m.endStoreBusy(result.Robots[i].UID)
	}
	return result, nil
}
