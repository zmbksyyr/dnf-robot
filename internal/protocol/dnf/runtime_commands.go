package dnf

import "robot/internal/shared"

func robotIsRunning(task *RobotDnfTask, uid int) bool {
	if task == nil {
		return false
	}
	vo := task.Find(uid)
	if vo == nil {
		return false
	}
	vo.mu.Lock()
	defer vo.mu.Unlock()
	return vo.State == StateRun
}

func (dt *DnfTableDrive) DispatchLogout(uid int) DnfTableTaskResult {
	return dt.dispatchLogouts(dt.task, []int{uid})
}

func (dt *DnfTableDrive) dispatchLogouts(task *RobotDnfTask, uids []int) DnfTableTaskResult {
	if task == nil || len(uids) == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	for _, uid := range uids {
		if uid <= 0 {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
	}
	for _, uid := range uids {
		if !task.TryAddMessage("MsgLogout", uid) {
			return DnfTableTaskResult{Msg: "logout queue full"}
		}
	}
	return DnfTableTaskResult{Code: 200}
}

func (dt *DnfTableDrive) DispatchMove(command shared.RuntimeMoveCommand) DnfTableTaskResult {
	return dt.dispatchMoves(dt.task, []shared.RuntimeMoveCommand{command})
}

func (dt *DnfTableDrive) dispatchMoves(task *RobotDnfTask, commands []shared.RuntimeMoveCommand) DnfTableTaskResult {
	if task == nil || len(commands) == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	for _, command := range commands {
		if command.UID <= 0 || !uint8Value(command.Village) || !uint8Value(command.Area) ||
			!uint16Value(command.X) || !uint16Value(command.Y) ||
			!uint8Value(command.MoveType) || !uint16Value(command.Speed) {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		if !robotIsRunning(task, command.UID) {
			return DnfTableTaskResult{Msg: "robot not online"}
		}
	}
	for _, command := range commands {
		if !task.TryAddMessage("MsgMove", &moveInternalData{
			ID:       command.UID,
			Village:  uint8(command.Village),
			Area:     uint8(command.Area),
			X:        uint16(command.X),
			Y:        uint16(command.Y),
			MoveType: uint8(command.MoveType),
			Speed:    uint16(command.Speed),
		}) {
			return DnfTableTaskResult{Msg: "move queue full"}
		}
	}
	return DnfTableTaskResult{Msg: "ok", Code: 200}
}

func (dt *DnfTableDrive) DispatchShout(command shared.RuntimeShoutCommand) DnfTableTaskResult {
	return dt.dispatchShouts(dt.task, []shared.RuntimeShoutCommand{command})
}

func (dt *DnfTableDrive) dispatchShouts(task *RobotDnfTask, commands []shared.RuntimeShoutCommand) DnfTableTaskResult {
	if task == nil || len(commands) == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	for _, command := range commands {
		if command.UID <= 0 || command.Message == "" {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		if !robotIsRunning(task, command.UID) {
			return DnfTableTaskResult{Msg: "robot not online"}
		}
	}
	for _, command := range commands {
		if !task.TryAddMessage("MsgPublicMsg", &publicMsgInternalData{
			ID:   command.UID,
			Msg:  command.Message,
			Type: command.Type,
		}) {
			return DnfTableTaskResult{Msg: "shout queue full"}
		}
	}
	return DnfTableTaskResult{Code: 200}
}

func uint8Value(value int) bool {
	return value >= 0 && value <= 1<<8-1
}

func uint16Value(value int) bool {
	return value >= 0 && value <= 1<<16-1
}
