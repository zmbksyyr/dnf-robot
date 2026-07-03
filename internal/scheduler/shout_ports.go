package scheduler

import (
	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/capability/robotruntime"
	robottemplate "robot/internal/capability/robottemplate"
)

func (m *RobotManager) shoutService() robotaction.ShoutService {
	return robotaction.ShoutService{Env: shoutActionEnv{manager: m}}
}

type shoutActionEnv struct {
	manager *RobotManager
}

func (e shoutActionEnv) AppendShout(uid int, channel string, typ int, msg string) error {
	return e.AppendShoutDetail(uid, channel, typ, msg, "normal")
}

func (e shoutActionEnv) AppendShoutDetail(uid int, channel string, typ int, msg, source string) error {
	if channel == "" {
		channel = "world"
	}
	_ = msg
	robotLogf("[Shout] source=%s uid=%d channel=%s type=%d\n", source, uid, channel, typ)
	return nil
}

func (e shoutActionEnv) Config() robotconfig.RuntimeConfig {
	return e.manager.loadRobotConfig()
}

func (e shoutActionEnv) LookupRobotName(uid int) string {
	name, _ := e.manager.schemaRepo().RobotCharacterName(uid)
	return name
}

func (e shoutActionEnv) RandBetween(min, max int) int {
	return e.manager.randBetween(min, max)
}

func (e shoutActionEnv) RandIntn(n int) int {
	return e.manager.randIntn(n)
}

func (e shoutActionEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMap()
}

func (e shoutActionEnv) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	return e.manager.repo().SelectRobots(req)
}

func (e shoutActionEnv) SendLocalShout(source string, uid int, msg string, msgType int) error {
	return robotruntime.LocalShout(e.manager.doll, source, uid, msg, msgType)
}

func (e shoutActionEnv) SendWorldShout(msg, name string, senderID uint16) error {
	return e.manager.worldShout.SendWorldShout(msg, name, senderID)
}

func (e shoutActionEnv) Templates() robottemplate.ShoutTemplates {
	return e.manager.loadShoutTemplates()
}
