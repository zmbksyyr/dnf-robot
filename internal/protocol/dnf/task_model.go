package dnf

import "encoding/json"

type MsgType int

const (
	MsgError     MsgType = 6000
	MsgList      MsgType = 6001
	MsgCreate    MsgType = 6002
	MsgRemove    MsgType = 6003
	MsgLogin     MsgType = 6004
	MsgLogout    MsgType = 6005
	MsgMove      MsgType = 6006
	MsgPublicMsg MsgType = 6007
	MsgOnLine    MsgType = 6008
)

type DnfTableTaskResult struct {
	Msg  string
	Code int
	UID  int
}

type RobotMsg struct {
	MsgFlag uint8
	Fd      int
	MsgType MsgType
	JSON    json.RawMessage
}

type RobotStallConfig struct {
	CfgContent   string
	CfgType      int
	UID          int
	FunctionType int
}
