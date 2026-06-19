package service

import (
	"errors"
	"fmt"
	"time"
)

var errActorRegistryUnavailable = errors.New("actor registry unavailable")

func (m *RobotManager) OnlineManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	registry := m.currentActorRegistry()
	if registry == nil {
		return RobotCommandResult{}, errActorRegistryUnavailable
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	rc := m.loadRobotConfig()
	registry.EnsureActorSlots(rc, rc.AutoTargetOnlineCount)
	result := newCommandResult(len(robots))
	timeout := time.Duration(rc.SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if !registry.HasUID(robot.UID) {
			if !registry.AttachUID(robot.UID, 10*time.Second) {
				result.Failed++
				result.Robots = append(result.Robots, actorFullResult(robot, "online"))
				continue
			}
		}
		item, ok := registry.Command(robot.UID, robotActorCmdOnline, timeout)
		item.CID = robot.CID
		if ok && (item.OK || item.State == "accepted" || item.State == "running") {
			result.Accepted++
			result.Robots = append(result.Robots, robotActionResult(robot, false, "accepted", ""))
		} else {
			result.Failed++
			result.Robots = append(result.Robots, failedActorResult(robot, item, "online actor command failed"))
		}
	}
	m.waitManagedConfirm(&result, time.Duration(rc.OnlineConfirmTimeoutMS)*time.Millisecond)
	return result, nil
}

func (m *RobotManager) MoveManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	return m.actorCommandManaged(req, robotActorMove, "move")
}

func (m *RobotManager) ShoutManaged(req RobotCommandRequest, world bool) (RobotCommandResult, error) {
	if world {
		return m.actorCommandManaged(req, robotActorShoutWorld, "shout_world")
	}
	cmd := robotActorShoutLocal
	name := "shout_local"
	return m.actorCommandManaged(req, cmd, name)
}

func (m *RobotManager) ShoutBothManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	registry := m.currentActorRegistry()
	if registry == nil {
		return RobotCommandResult{}, errActorRegistryUnavailable
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	timeout := time.Duration(m.loadRobotConfig().SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if !registry.HasUID(robot.UID) {
			result.Failed++
			result.Robots = append(result.Robots, uidNotAttachedResult(robot, "shout"))
			continue
		}
		local, localOK := registry.Command(robot.UID, robotActorShoutLocal, timeout)
		world, worldOK := registry.Command(robot.UID, robotActorShoutWorld, timeout)
		if localOK && worldOK && local.OK && world.OK {
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, robotActionResult(robot, true, "sent", ""))
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
			result.Robots = append(result.Robots, robotActionResult(robot, false, "failed", msg))
		}
	}
	return result, nil
}

func (m *RobotManager) StoreManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	return m.actorCommandManaged(req, robotActorStore, "store")
}

func (m *RobotManager) LogoutManaged(req RobotCommandRequest) (RobotCommandResult, error) {
	registry := m.currentActorRegistry()
	if registry == nil {
		return RobotCommandResult{}, errActorRegistryUnavailable
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	timeout := time.Duration(m.loadRobotConfig().SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if registry.HasUID(robot.UID) {
			item, ok := registry.LogoutUID(robot.UID, timeout)
			item.CID = robot.CID
			if ok && item.OK {
				result.Accepted++
				result.Confirmed++
				result.Robots = append(result.Robots, item)
			} else {
				result.Failed++
				result.Robots = append(result.Robots, failedActorResult(robot, item, "logout actor command failed"))
			}
			continue
		}
		result.Failed++
		result.Robots = append(result.Robots, uidNotAttachedResult(robot, "logout"))
	}
	return result, nil
}

func (m *RobotManager) actorCommandManaged(req RobotCommandRequest, cmd robotActorCommand, action string) (RobotCommandResult, error) {
	registry := m.currentActorRegistry()
	if registry == nil {
		return RobotCommandResult{}, errActorRegistryUnavailable
	}
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	result := newCommandResult(len(robots))
	timeout := time.Duration(m.loadRobotConfig().SystemManualActionTimeoutSec) * time.Second
	for _, robot := range robots {
		if !registry.HasUID(robot.UID) {
			result.Failed++
			result.Robots = append(result.Robots, uidNotAttachedResult(robot, action))
			continue
		}
		item, ok := registry.Command(robot.UID, cmd, timeout)
		item.CID = robot.CID
		if ok && item.OK {
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, item)
		} else {
			result.Failed++
			result.Robots = append(result.Robots, failedActorResult(robot, item, fmt.Sprintf("%s actor command failed", action)))
		}
	}
	return result, nil
}

func robotActionResult(robot RobotInfo, ok bool, state, message string) RobotActionResult {
	return RobotActionResult{UID: robot.UID, CID: robot.CID, OK: ok, State: state, Message: message}
}

func failedActorResult(robot RobotInfo, item RobotActionResult, fallbackMessage string) RobotActionResult {
	if item.UID == 0 {
		item.UID = robot.UID
	}
	if item.CID == 0 {
		item.CID = robot.CID
	}
	if item.State == "" {
		item.State = "failed"
	}
	if item.Message == "" {
		item.Message = fallbackMessage
	}
	return item
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
