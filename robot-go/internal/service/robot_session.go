package service

import (
	"encoding/json"
	"fmt"
	"time"
)

func (m *RobotManager) Online(req RobotCommandRequest, store bool) (RobotCommandResult, error) {
	return m.online(req, store, true)
}

func (m *RobotManager) OnlineNoConfirm(req RobotCommandRequest, store bool) (RobotCommandResult, error) {
	return m.online(req, store, false)
}

func (m *RobotManager) online(req RobotCommandRequest, store bool, confirm bool) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	rc := m.loadRobotConfig()
	if rc.SystemPacketRatePerSec > 0 {
		minInterval := 1000 / rc.SystemPacketRatePerSec
		if minInterval > 0 && rc.OnlineDispatchIntervalMS < minInterval {
			rc.OnlineDispatchIntervalMS = minInterval
		}
	}
	if !store {
		if len(robots) > rc.MaxOnlinePerCommand {
			return result, fmt.Errorf("requested %d robots exceeds max_online_per_command=%d", len(robots), rc.MaxOnlinePerCommand)
		}
		running := m.countRuntimeRunning()
		alreadyRunning := 0
		status := m.runtimeStatusMap()
		for _, r := range robots {
			if st, ok := status[r.UID]; ok && activeRuntimeStatus(st) {
				alreadyRunning++
			}
		}
		newLogins := len(robots) - alreadyRunning
		if running+newLogins > rc.MaxOnlineRobots {
			return result, fmt.Errorf("online limit exceeded: running=%d new=%d max_online_robots=%d", running, newLogins, rc.MaxOnlineRobots)
		}
	}
	if rc.OnlineDispatchIntervalMS <= 0 {
		userinfos := make([]map[string]interface{}, 0, len(robots))
		for _, r := range robots {
			if err := m.ensureRobotWorldHorn(r.UID); err != nil {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
				continue
			}
			userinfos = append(userinfos, map[string]interface{}{
				"birtharea": r.Area, "birthvill": r.Village, "birthx": r.X, "birthy": r.Y,
				"cid": 0, "delay": rc.LoginDelayMS, "discost": 0, "disopen": 0,
				"id": 0, "ip": m.robotConnectIP(), "maxreconn": rc.MaxReconnect, "port": r.Port,
				"redelay": rc.ReconnectDelayMS, "storeopen": boolToInt(store),
				"storetitle": fmt.Sprintf("bot-%d-store", r.UID), "uid": r.UID,
			})
			result.Accepted++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted"})
		}
		body := map[string]interface{}{"userinfos": userinfos}
		data, _ := json.Marshal(body)
		if _, err := m.doll.MsgOnLine("manager", string(data)); err != nil {
			result.Failed = result.Accepted
			result.Accepted = 0
			for i := range result.Robots {
				result.Robots[i].State = "failed"
				result.Robots[i].Message = err.Error()
			}
		}
	} else {
		for _, r := range robots {
			if err := m.ensureRobotWorldHorn(r.UID); err != nil {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
				continue
			}
			body := map[string]interface{}{"userinfos": []map[string]interface{}{{
				"birtharea": r.Area, "birthvill": r.Village, "birthx": r.X, "birthy": r.Y,
				"cid": 0, "delay": rc.LoginDelayMS, "discost": 0, "disopen": 0,
				"id": 0, "ip": m.robotConnectIP(), "maxreconn": rc.MaxReconnect, "port": r.Port,
				"redelay": rc.ReconnectDelayMS, "storeopen": boolToInt(store),
				"storetitle": fmt.Sprintf("bot-%d-store", r.UID), "uid": r.UID,
			}}}
			data, _ := json.Marshal(body)
			if _, err := m.doll.MsgOnLine("manager", string(data)); err == nil {
				result.Accepted++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted"})
			} else {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
			}
			time.Sleep(time.Duration(rc.OnlineDispatchIntervalMS) * time.Millisecond)
		}
	}
	if result.Accepted == 0 {
		return result, nil
	}
	if !confirm {
		return result, nil
	}
	deadline := time.Now().Add(time.Duration(rc.OnlineConfirmTimeoutMS) * time.Millisecond)
	status := m.runtimeStatusMap()
	for time.Now().Before(deadline) {
		confirmed := 0
		status = m.runtimeStatusMap()
		for _, robot := range result.Robots {
			if st, ok := status[robot.UID]; ok && activeRuntimeStatus(st) {
				confirmed++
			}
		}
		if confirmed >= result.Accepted {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	status = m.runtimeStatusMap()
	for i := range result.Robots {
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = st.StateName
			result.Confirmed++
		} else if result.Robots[i].State == "accepted" {
			result.Robots[i].State = "pending"
			result.Robots[i].Message = "not confirmed running"
			result.Failed++
		}
	}
	return result, nil
}

func (m *RobotManager) Logout(req RobotCommandRequest) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	for _, r := range robots {
		body := map[string]interface{}{"userinfos": []map[string]interface{}{{"id": r.UID}}}
		data, _ := json.Marshal(body)
		if _, err := m.doll.MsgLogout("manager", string(data)); err == nil {
			result.Accepted++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted"})
		} else {
			result.Failed++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
		}
	}
	time.Sleep(500 * time.Millisecond)
	status := m.runtimeStatusMap()
	for i := range result.Robots {
		if _, ok := status[result.Robots[i].UID]; !ok {
			result.Robots[i].OK = true
			result.Robots[i].State = "closed"
			result.Confirmed++
		} else if result.Robots[i].State == "accepted" {
			result.Robots[i].State = "pending"
			result.Robots[i].Message = "runtime connection still exists"
			result.Failed++
		}
	}
	return result, nil
}
