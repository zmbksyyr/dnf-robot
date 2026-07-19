package scheduler

import (
	robotcap "robot/internal/capability/robot"
	robotaction "robot/internal/capability/robotaction"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/capability/robotruntime"
	"robot/internal/shared"
)

func (m *RobotManager) moveService() robotaction.MoveService {
	return robotaction.MoveService{Env: moveActionEnv{manager: m}}
}

type moveActionEnv struct {
	manager *RobotManager
}

func (e moveActionEnv) DispatchMoveStep(info robotcap.Info, targetX, targetY, step, steps, speed int, rc robotconfig.RuntimeConfig) error {
	if err := robotruntime.MoveStep(e.manager.doll, info, targetX, targetY, step, steps, speed, rc); err != nil {
		return err
	}
	if step == steps {
		_ = e.manager.schemaRepo().UpdateRobotPosition(info, targetX, targetY)
	}
	return nil
}

func (e moveActionEnv) LoadMapCatalog() []shared.MapCatalogItem {
	return e.manager.loadMapCatalog()
}

func (e moveActionEnv) RandBetween(min, max int) int {
	return e.manager.randBetween(min, max)
}

func (e moveActionEnv) RuntimeStatus(uid int) (robotcap.RuntimeStatus, bool) {
	return e.manager.runtimeStatus(uid)
}

func (e moveActionEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMap()
}

func (e moveActionEnv) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	return e.manager.repo().SelectRobots(req)
}
