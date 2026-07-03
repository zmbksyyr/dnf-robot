package scheduler

import (
	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	"robot/internal/capability/robotruntime"
)

func (m *RobotManager) sessionService() robotaction.SessionService {
	return robotaction.SessionService{Env: sessionActionEnv{manager: m}}
}

type sessionActionEnv struct {
	manager *RobotManager
}

func (e sessionActionEnv) CountRuntimeRunning() int {
	return e.manager.countRuntimeRunning()
}

func (e sessionActionEnv) EnsureWorldHorn(uid int) error {
	return e.manager.storePreparer().EnsureWorldHorn(uid)
}

func (e sessionActionEnv) RobotConnectIP() string {
	return e.manager.robotConnectIP()
}

func (e sessionActionEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMap()
}

func (e sessionActionEnv) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	return e.manager.repo().SelectRobots(req)
}

func (e sessionActionEnv) SendLogout(uid int) error {
	return robotruntime.Logout(e.manager.doll, uid)
}

func (e sessionActionEnv) SendOnline(userinfos []map[string]interface{}) error {
	return robotruntime.Online(e.manager.doll, userinfos)
}
