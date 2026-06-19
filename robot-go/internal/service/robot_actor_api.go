package service

import (
	"fmt"
	"time"
)

func (m *RobotManager) OnlineManaged(req RobotCommandRequest, store bool) (RobotCommandResult, error) {
	if store {
		return m.Online(req, store)
	}
	supervisor := m.currentSupervisor()
	if supervisor == nil {
		return m.Online(req, store)
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	rc := m.loadRobotConfig()
	supervisor.ensureAutoActorSlots(rc, rc.AutoTargetOnlineCount)
	result := newCommandResult(len(robots))
	timeout := time.Duration(rc.SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if !supervisor.HasUID(robot.UID) {
			if !supervisor.AttachUID(robot.UID, 10*time.Second) {
				result.Failed++
				result.Robots = append(result.Robots, actorFullResult(robot, "online"))
				continue
			}
		}
		item, ok := supervisor.Command(robot.UID, robotActorCmdOnline, timeout)
		item.CID = robot.CID
		if ok && (item.OK || item.State == "accepted" || item.State == "running") {
			result.Accepted++
			result.Robots = append(result.Robots, RobotActionResult{UID: robot.UID, CID: robot.CID, OK: false, State: "accepted"})
		} else {
			result.Failed++
			if item.UID == 0 {
				item.UID = robot.UID
			}
			if item.State == "" {
				item.State = "failed"
			}
			if item.Message == "" {
				item.Message = "online actor command failed"
			}
			result.Robots = append(result.Robots, item)
		}
	}
	m.waitManagedConfirm(&result, time.Duration(rc.OnlineConfirmTimeoutMS)*time.Millisecond)
	return result, nil
}

func (m *RobotManager) MoveManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	return m.actorCommandManaged(req, robotActorMove, "move", func(single RobotCommandRequest) (RobotCommandResult, error) {
		return m.Move(single)
	})
}

func (m *RobotManager) ShoutManaged(req RobotCommandRequest, world bool) (RobotCommandResult, error) {
	if world {
		return m.actorCommandManaged(req, robotActorShoutWorld, "shout_world", func(single RobotCommandRequest) (RobotCommandResult, error) {
			return m.ShoutOne(single, true)
		})
	}
	cmd := robotActorShoutLocal
	name := "shout_local"
	return m.actorCommandManaged(req, cmd, name, func(single RobotCommandRequest) (RobotCommandResult, error) {
		return m.ShoutOne(single, world)
	})
}

func (m *RobotManager) ShoutBothManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	supervisor := m.currentSupervisor()
	if supervisor == nil {
		return m.Shout(req)
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	timeout := time.Duration(m.loadRobotConfig().SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if !supervisor.HasUID(robot.UID) {
			result.Failed++
			result.Robots = append(result.Robots, uidNotAttachedResult(robot, "shout"))
			continue
		}
		local, localOK := supervisor.Command(robot.UID, robotActorShoutLocal, timeout)
		world, worldOK := supervisor.Command(robot.UID, robotActorShoutWorld, timeout)
		if localOK && worldOK && local.OK && world.OK {
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, RobotActionResult{UID: robot.UID, CID: robot.CID, OK: true, State: "sent"})
		} else {
			result.Failed++
			msg := "actor command failed"
			if !localOK || !local.OK {
				msg = local.Message
				if msg == "" {
					msg = local.State
				}
			} else if !worldOK || !world.OK {
				msg = world.Message
				if msg == "" {
					msg = world.State
				}
			}
			result.Robots = append(result.Robots, RobotActionResult{UID: robot.UID, CID: robot.CID, OK: false, State: "failed", Message: msg})
		}
	}
	return result, nil
}

func (m *RobotManager) StoreManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	return m.actorCommandManaged(req, robotActorStore, "store", func(single RobotCommandRequest) (RobotCommandResult, error) {
		return m.Store(single)
	})
}

func (m *RobotManager) LogoutManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	supervisor := m.currentSupervisor()
	if supervisor == nil {
		return m.Logout(req)
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	timeout := time.Duration(m.loadRobotConfig().SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if supervisor.HasUID(robot.UID) {
			item, ok := supervisor.LogoutUID(robot.UID, timeout)
			item.CID = robot.CID
			if ok && item.OK {
				result.Accepted++
				result.Confirmed++
				result.Robots = append(result.Robots, item)
			} else {
				result.Failed++
				if item.UID == 0 {
					item.UID = robot.UID
				}
				if item.State == "" {
					item.State = "failed"
				}
				if item.Message == "" {
					item.Message = "logout actor command failed"
				}
				result.Robots = append(result.Robots, item)
			}
			continue
		}
		result.Failed++
		result.Robots = append(result.Robots, uidNotAttachedResult(robot, "logout"))
	}
	return result, nil
}

func (m *RobotManager) actorCommandManaged(req RobotCommandRequest, cmd robotActorCommand, action string, fallback func(RobotCommandRequest) (RobotCommandResult, error)) (RobotCommandResult, error) {
	supervisor := m.currentSupervisor()
	if supervisor == nil {
		return fallback(req)
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	timeout := time.Duration(m.loadRobotConfig().SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if !supervisor.HasUID(robot.UID) {
			result.Failed++
			result.Robots = append(result.Robots, uidNotAttachedResult(robot, action))
			continue
		}
		item, ok := supervisor.Command(robot.UID, cmd, timeout)
		item.CID = robot.CID
		if ok && item.OK {
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, item)
		} else {
			result.Failed++
			if item.UID == 0 {
				item.UID = robot.UID
			}
			if item.State == "" {
				item.State = "failed"
			}
			if item.Message == "" {
				item.Message = fmt.Sprintf("%s actor command failed", action)
			}
			result.Robots = append(result.Robots, item)
		}
	}
	return result, nil
}

func uidNotAttachedResult(robot RobotInfo, action string) RobotActionResult {
	return RobotActionResult{
		UID:     robot.UID,
		CID:     robot.CID,
		OK:      false,
		State:   "uid_not_attached",
		Message: fmt.Sprintf("%s requires actor attachment", action),
	}
}

func actorFullResult(robot RobotInfo, action string) RobotActionResult {
	return RobotActionResult{
		UID:     robot.UID,
		CID:     robot.CID,
		OK:      false,
		State:   "actor_full",
		Message: fmt.Sprintf("%s requires an empty actor slot", action),
	}
}

func (m *RobotManager) currentSupervisor() *RobotSupervisor {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.supervisor
}

func (m *RobotManager) waitManagedConfirm(result *RobotCommandResult, timeout time.Duration) {
	if result == nil || result.Accepted == 0 {
		return
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		confirmed := 0
		status := m.runtimeStatusMap()
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
	status := m.runtimeStatusMap()
	result.Confirmed = 0
	result.Failed = 0
	for i := range result.Robots {
		if result.Robots[i].State != "accepted" {
			if !result.Robots[i].OK {
				result.Failed++
			}
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = "running"
			result.Confirmed++
		} else {
			result.Robots[i].State = "pending"
			result.Robots[i].Message = "not confirmed running"
			result.Failed++
		}
	}
}
