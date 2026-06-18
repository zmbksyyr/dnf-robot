package dnf

import (
	"robot/pkg/message"
)

func (dt *DnfTableDrive) handlePublicMsg(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgPublicMsgRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() || !userInfo.HasMsg() || len(userInfo.Msg()) == 0 {
			return DnfTableTaskResult{Msg: "校验失败"}
		}

		if !robotIsRunning(task, userInfo.Id()) {
			return DnfTableTaskResult{Msg: "robot not online"}
		}

		md := &publicMsgInternalData{
			ID:   userInfo.Id(),
			Msg:  userInfo.Msg(),
			Type: userInfo.TypeValue(),
		}
		task.AddMessage("MsgPublicMsg", md)
	}

	return DnfTableTaskResult{Code: 200}
}
