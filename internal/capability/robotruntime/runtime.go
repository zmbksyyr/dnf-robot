package robotruntime

import (
	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	robotconfig "robot/internal/capability/robotconfig"
)

type Session interface {
	MsgLogout(clientID string, keyData string) (string, error)
	MsgOnLine(clientID string, keyData string) (string, error)
}

type Move interface {
	MsgMove(clientID string, keyData string) (string, error)
}

type Shout interface {
	MsgPublicMsg(clientID string, keyData string) (string, error)
}

type Store interface {
	StartPrivateStore(uid int, title string) bool
	ResetPrivateStore(uid int) bool
	SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool
	CompletePrivateStoreDisplay(uid int) bool
}

func Logout(runtime Session, uid int) error {
	_, err := runtime.MsgLogout("manager", robotaction.LogoutPayload(uid))
	return err
}

func Online(runtime Session, userinfos []map[string]interface{}) error {
	_, err := runtime.MsgOnLine("manager", robotaction.OnlinePayload(userinfos))
	return err
}

func MoveStep(runtime Move, info robotcap.Info, targetX, targetY, step, steps, speed int, rc robotconfig.RuntimeConfig) error {
	_, err := runtime.MsgMove("manager", robotaction.MovePayload(info, targetX, targetY, step, steps, speed, rc))
	return err
}

func LocalShout(runtime Shout, source string, uid int, msg string, msgType int) error {
	_, err := runtime.MsgPublicMsg(source, robotaction.LocalShoutPayload(uid, msg, msgType))
	return err
}

func StartPrivateStore(runtime Store, uid int, title string) bool {
	return runtime.StartPrivateStore(uid, title)
}

func ResetPrivateStore(runtime Store, uid int) bool {
	return runtime.ResetPrivateStore(uid)
}

func SetAreaFrom(runtime Store, uid int, village, area int, x, y int, fromVillage, fromArea int) bool {
	return runtime.SetAreaFrom(uid, village, area, x, y, fromVillage, fromArea)
}

func CompletePrivateStoreDisplay(runtime Store, uid int) bool {
	return runtime.CompletePrivateStoreDisplay(uid)
}
