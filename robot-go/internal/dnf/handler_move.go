package dnf

import (
	"robot/pkg/message"
)

func (dt *DnfTableDrive) handleMove(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgMoveRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() || !userInfo.HasVillage() || !userInfo.HasArea() ||
			!userInfo.HasX() || !userInfo.HasY() {
			return DnfTableTaskResult{Msg: "校验失败"}
		}

		if !robotIsRunning(task, userInfo.Id()) {
			return DnfTableTaskResult{Msg: "robot not online"}
		}

		md := &moveInternalData{
			ID:       userInfo.Id(),
			Village:  uint8(userInfo.GetVillage()),
			Area:     uint8(userInfo.GetArea()),
			X:        uint16(userInfo.GetX()),
			Y:        uint16(userInfo.GetY()),
			MoveType: uint8(userInfo.TypeVal()),
			Speed:    uint16(userInfo.GetSpeed()),
		}
		task.AddMessage("MsgMove", md)
	}

	return DnfTableTaskResult{Msg: "ok", Code: 200}
}

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
