package scheduler

import (
	"fmt"
	"robot/internal/capability/catalog"
	equipcap "robot/internal/capability/equipment"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/capability/robotspawn"
	robottemplate "robot/internal/capability/robottemplate"
	"robot/internal/shared"
)

func (m *RobotManager) robotName(uid int, used map[string]struct{}, rc robotconfig.RuntimeConfig) string {
	return robottemplate.AllocateName(uid, used, rc, m.loadNameTemplates(), func(dbName string) bool {
		exists, _ := m.schemaRepo().CharacterNameExists(dbName)
		return exists
	}, m.randomString, m.randBetween)
}

func (m *RobotManager) CreateRobots(req robotcap.CreateRequest) ([]robotcap.Info, error) {
	var robots []robotcap.Info
	var err error
	_ = m.lockHub().WithResource("scheduler", "lifecycle", "create_robots", func() error {
		robots, err = m.createRobotsLocked(req)
		return nil
	})
	return robots, err
}

func (m *RobotManager) createRobotsLocked(req robotcap.CreateRequest) ([]robotcap.Info, error) {
	_, finishOperation, err := m.beginTrackedStructuralOperation("create", fmt.Sprintf("count=%d", req.Count))
	if err != nil {
		return nil, err
	}
	var opErr error
	var robots []robotcap.Info
	defer func() {
		finishOperation(fmt.Sprintf("created=%d", len(robots)), opErr)
	}()
	robots, err = m.lifecycleCreator().Create(req)
	opErr = err
	return robots, err
}

func (m *RobotManager) equipFromCatalog(cid int, level int, job int, rc robotconfig.RuntimeConfig) error {
	items := m.loadEquipmentCatalog()
	if len(items) == 0 {
		return nil
	}
	raw := equipcap.BuildEquipmentSlots(items, level, job, rc, m.randIntn, m.withRand)
	return m.schemaRepo().SaveEquipmentSlots(cid, raw)
}

func (m *RobotManager) avatarFromCatalog(cid int, level int, job int, rc robotconfig.RuntimeConfig) error {
	items := m.loadEquipmentCatalog()
	if len(items) == 0 {
		return nil
	}
	selected := equipcap.SelectAvatar(items, job, rc, m.randIntn)
	if rc.MinAvatarSlots > 0 && len(selected) < rc.MinAvatarSlots {
		return nil
	}
	return m.schemaRepo().ReplaceAvatarItems(cid, selected)
}

func (m *RobotManager) loadEquipmentCatalog() []shared.EquipmentCatalogItem {
	if m.cfg == nil {
		return nil
	}
	return catalog.Equipment(m.cfg.ConfigDir)
}

func (m *RobotManager) loadStackableCatalog() []shared.EquipmentCatalogItem {
	if m.cfg == nil {
		return nil
	}
	return catalog.Stackable(m.cfg.ConfigDir)
}

func (m *RobotManager) applyConfiguredLocation(info *robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) {
	robotspawn.ApplyConfiguredLocation(spawnEnv{manager: m}, info, rc, maps)
}

func (m *RobotManager) randomMap(maps []shared.MapCatalogItem, level int) (shared.MapCatalogItem, bool) {
	return robotspawn.RandomMap(spawnEnv{manager: m}, maps, level)
}

type spawnEnv struct {
	manager *RobotManager
}

func (e spawnEnv) FollowAccountVillage(account string) (int, bool, error) {
	return e.manager.schemaRepo().FollowAccountVillage(account)
}

func (e spawnEnv) RandBetween(min, max int) int {
	return e.manager.randBetween(min, max)
}

func (e spawnEnv) RandIntn(n int) int {
	return e.manager.randIntn(n)
}
