package dnf

import (
	"time"

	"robot/pkg/message"
)

func (dt *DnfTableDrive) handleList(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult {
	req := message.NewMsgListRequest()

	if err := req.ParseFromString(string(robotMsg.JSON)); err != nil {
		return DnfTableTaskResult{Msg: "校验失败"}
	}

	_ = req

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
