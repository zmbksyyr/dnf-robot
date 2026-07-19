package dnf

import (
	"time"

	"robot/internal/foundation/message"
	"robot/internal/shared"
)

const invalidRequestMessage = "invalid request"

func (dt *DnfTableDrive) handleList(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgListRequest()
	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}

	respond := message.NewMsgListRespond()
	robotMap := task.GetRobotVoMap()
	for _, vo := range robotMap {
		vo.mu.Lock()

		result := message.NewMsgListResult()
		result.SetId(int(vo.UID))
		result.SetIp(vo.IP)
		result.SetPort(vo.Port)
		result.SetUid(int(vo.UID))
		result.SetCid(int(vo.CID))
		result.SetConncount(int(vo.ConnCount))
		result.SetMaxreconn(int(vo.MaxReConn))
		result.SetUserstate(message.MsgListUserState(vo.State))
		result.SetLasterror(message.MsgListUserError(vo.LastError))
		result.SetCurvill(int(vo.CurVillage))
		result.SetCurarea(int(vo.CurArea))
		result.SetCurx(int(vo.CurX))
		result.SetCury(int(vo.CurY))
		result.SetRobotType(vo.RobotTyp)

		if vo.State == StateRun {
			result.SetRuntime(int(uint32(time.Now().Unix()) - vo.RunStartTime))
		} else {
			result.SetRuntime(0)
		}

		vo.mu.Unlock()
		respond.AddUserstatus(result)
	}

	jsonStr, _ := respond.SerializeToString()
	return DnfTableTaskResult{Code: 200, Msg: jsonStr}
}

func (dt *DnfTableDrive) handleLogout(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgLogoutRequest()
	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}

	uids := make([]int, 0, req.UserinfosSize())
	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		uids = append(uids, userInfo.Id())
	}
	return dt.dispatchLogouts(task, uids)
}

func (dt *DnfTableDrive) handleMove(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgMoveRequest()
	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}

	commands := make([]shared.RuntimeMoveCommand, 0, req.UserinfosSize())
	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() || !userInfo.HasVillage() || !userInfo.HasArea() ||
			!userInfo.HasX() || !userInfo.HasY() {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		commands = append(commands, shared.RuntimeMoveCommand{
			UID:      userInfo.Id(),
			Village:  userInfo.GetVillage(),
			Area:     userInfo.GetArea(),
			X:        userInfo.GetX(),
			Y:        userInfo.GetY(),
			MoveType: userInfo.TypeVal(),
			Speed:    userInfo.GetSpeed(),
		})
	}
	return dt.dispatchMoves(task, commands)
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

func (dt *DnfTableDrive) handlePublicMsg(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgPublicMsgRequest()
	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}

	commands := make([]shared.RuntimeShoutCommand, 0, req.UserinfosSize())
	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() || !userInfo.HasMsg() || len(userInfo.Msg()) == 0 {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		commands = append(commands, shared.RuntimeShoutCommand{
			UID:     userInfo.Id(),
			Message: userInfo.Msg(),
			Type:    userInfo.TypeValue(),
		})
	}
	return dt.dispatchShouts(task, commands)
}

func (dt *DnfTableDrive) handleRemove(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgRemoveRequest()
	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}
	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: invalidRequestMessage}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() {
			return DnfTableTaskResult{Msg: invalidRequestMessage}
		}
		task.AddMessage("MsgLogout", userInfo.Id())
	}

	return DnfTableTaskResult{Code: 200}
}
