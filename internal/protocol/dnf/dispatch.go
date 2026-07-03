package dnf

import (
	"fmt"
)

// ---- dispatch.go ----
type HandlerFunc func(task *RobotDnfTask, robotMsg RobotMsg) DnfTableTaskResult

type DnfTableDrive struct {
	dispatchers map[MsgType]HandlerFunc
	task        *RobotDnfTask
}

func NewDnfTableDrive() *DnfTableDrive {
	task := NewRobotDnfTask()
	dt := &DnfTableDrive{
		dispatchers: make(map[MsgType]HandlerFunc),
		task:        task,
	}

	dt.dispatchers[MsgOnLine] = dt.handleOnLine
	dt.dispatchers[MsgMove] = dt.handleMove
	dt.dispatchers[MsgRemove] = dt.handleRemove
	dt.dispatchers[MsgPublicMsg] = dt.handlePublicMsg
	dt.dispatchers[MsgLogout] = dt.handleLogout
	dt.dispatchers[MsgList] = dt.handleList

	return dt
}

func (dt *DnfTableDrive) HandleKeyword(robotMsg RobotMsg) DnfTableTaskResult {
	handler, ok := dt.dispatchers[robotMsg.MsgType]
	if !ok {
		fmt.Printf("msgType:%d not found\n", robotMsg.MsgType)
		return DnfTableTaskResult{
			Msg:  fmt.Sprintf("msgType:%d not found", robotMsg.MsgType),
			Code: 0,
		}
	}
	return handler(dt.task, robotMsg)
}

func (dt *DnfTableDrive) GetTask() *RobotDnfTask {
	return dt.task
}

func (dt *DnfTableDrive) Shutdown() {
	dt.task.Shutdown()
}
