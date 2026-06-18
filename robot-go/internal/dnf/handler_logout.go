package dnf

import (
	"robot/pkg/message"
)

func (dt *DnfTableDrive) handleLogout(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgLogoutRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	if req.UserinfosSize() == 0 {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	for i := 0; i < req.UserinfosSize(); i++ {
		userInfo := req.Userinfos(i)
		if !userInfo.HasId() {
			return DnfTableTaskResult{Msg: "校验失败"}
		}

		task.AddMessage("MsgLogout", userInfo.Id())
	}

	return DnfTableTaskResult{Code: 200}
}
