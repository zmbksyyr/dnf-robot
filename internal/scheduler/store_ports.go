package scheduler

import (
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/capability/robotruntime"
	storecap "robot/internal/capability/store"
	"robot/internal/shared"
)

func (m *RobotManager) storeWorkflow() storecap.Workflow {
	return storecap.Workflow{Env: storeWorkflowEnv{manager: m}}
}

type storeWorkflowEnv struct {
	manager *RobotManager
}

func (e storeWorkflowEnv) AddAutoStore(success, failed, expired int) {
	e.manager.addAutoStore(success, failed, expired)
}

func (e storeWorkflowEnv) AcquireAutoStoreSlot(rc robotconfig.RuntimeConfig) (func(), bool) {
	return e.manager.acquireAutoItemStoreSlot(rc)
}

func (e storeWorkflowEnv) BeginStoreBusy(uid int) bool {
	return e.manager.beginStoreBusy(uid)
}

func (e storeWorkflowEnv) CompletePrivateStoreDisplay(uid int) bool {
	return robotruntime.CompletePrivateStoreDisplay(e.manager.doll, uid)
}

func (e storeWorkflowEnv) Config() robotconfig.RuntimeConfig {
	return e.manager.loadRobotConfig()
}

func (e storeWorkflowEnv) EndStoreBusy(uid int) {
	e.manager.endStoreBusy(uid)
}

func (e storeWorkflowEnv) EnsureStoreInventoryAndStall(info robotcap.Info, rc robotconfig.RuntimeConfig) error {
	return e.manager.storePreparer().EnsureInventoryAndStall(info, rc)
}

func (e storeWorkflowEnv) FinishStoreState(uid, cid int, reason string) {
	e.manager.finishStoreState(uid, cid, reason)
}

func (e storeWorkflowEnv) Logf(format string, args ...interface{}) {
	robotLogf(format, args...)
}

func (e storeWorkflowEnv) Logout(req robotcap.CommandRequest) (robotcap.CommandResult, error) {
	return e.manager.sessionService().Logout(req)
}

func (e storeWorkflowEnv) MarkStoreStarted(uid int) error {
	return e.manager.schemaRepo().MarkStoreStarted(uid)
}

func (e storeWorkflowEnv) Online(req robotcap.CommandRequest, store bool, confirm bool) (robotcap.CommandResult, error) {
	return e.manager.sessionService().Online(req, store, confirm, e.manager.loadRobotConfig())
}

func (e storeWorkflowEnv) PrepareStorePosition(info robotcap.Info) error {
	return e.manager.schemaRepo().PrepareStorePosition(info)
}

func (e storeWorkflowEnv) RestoreAutoNormalOnline(info robotcap.Info, rc robotconfig.RuntimeConfig, reason string) (robotcap.Info, bool) {
	return e.manager.restoreAutoNormalOnline(info, rc, reason)
}

func (e storeWorkflowEnv) RobotGamePort() int {
	return e.manager.cfg.RobotGamePort
}

func (e storeWorkflowEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return e.manager.runtimeStatusMap()
}

func (e storeWorkflowEnv) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	return e.manager.repo().SelectRobots(req)
}

func (e storeWorkflowEnv) SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool {
	return robotruntime.SetAreaFrom(e.manager.doll, uid, village, area, x, y, fromVillage, fromArea)
}

func (e storeWorkflowEnv) StartPrivateStore(uid int, title string) bool {
	return robotruntime.StartPrivateStore(e.manager.doll, uid, title)
}

func (e storeWorkflowEnv) StorePoints() *storecap.PointCoordinator {
	return e.manager.storePoints()
}

func (e storeWorkflowEnv) SyncRobotCharacterVillage(cid int, village int) error {
	statPrev, err := e.manager.schemaRepo().SyncCharacterVillage(cid, village)
	if err != nil {
		return err
	}
	robotLogf("[AutoStore] cid=%d charac_village_synced village=%d stat_prev=%d\n", cid, village, statPrev)
	return nil
}

func (m *RobotManager) storePreparer() storecap.Preparer {
	return storecap.Preparer{Env: storePreparationEnv{manager: m}}
}

type storePreparationEnv struct {
	manager *RobotManager
}

func (e storePreparationEnv) EnsureStorePermissionRecord(uid, cid int) (storecap.PermissionStatus, error) {
	return e.manager.schemaRepo().EnsureStorePermission(uid, cid)
}

func (e storePreparationEnv) LoadInventory(cid int) ([]byte, error) {
	return e.manager.schemaRepo().LoadInventory(cid)
}

func (e storePreparationEnv) Logf(format string, args ...interface{}) {
	robotLogf(format, args...)
}

func (e storePreparationEnv) RandBetween(min, max int) int {
	return e.manager.randBetween(min, max)
}

func (e storePreparationEnv) RandShuffle(n int, swap func(i, j int)) {
	e.manager.randShuffle(n, swap)
}

func (e storePreparationEnv) ReplaceStoreStall(uid int, title string, items []storecap.StallItem) (storecap.StallResult, error) {
	return e.manager.schemaRepo().ReplaceStoreStall(uid, title, items)
}

func (e storePreparationEnv) RepairRobotExpBounds(uid, cid int) (storecap.ExpRepairResult, error) {
	return e.manager.schemaRepo().RepairRobotExpBounds(uid, cid)
}

func (e storePreparationEnv) RobotCID(uid int) (int, error) {
	return e.manager.schemaRepo().RobotCID(uid)
}

func (e storePreparationEnv) SaveInventory(cid int, capacity int, raw []byte) error {
	return e.manager.schemaRepo().SaveInventory(cid, capacity, raw)
}

func (e storePreparationEnv) SaveInventoryRaw(cid int, raw []byte) error {
	return e.manager.schemaRepo().SaveInventoryRaw(cid, raw)
}

func (e storePreparationEnv) StackableCatalog() []shared.EquipmentCatalogItem {
	return e.manager.loadStackableCatalog()
}

func (m *RobotManager) storeMaintenance() storecap.Maintenance {
	return storecap.Maintenance{Env: storeMaintenanceEnv{manager: m}}
}

type storeMaintenanceEnv struct {
	manager *RobotManager
}

func (e storeMaintenanceEnv) ApplyConfiguredLocation(info *robotcap.Info, rc robotconfig.RuntimeConfig, maps []shared.MapCatalogItem) {
	e.manager.applyConfiguredLocation(info, rc, maps)
}

func (e storeMaintenanceEnv) LoadMapCatalog() []shared.MapCatalogItem {
	return e.manager.loadMapCatalog()
}

func (e storeMaintenanceEnv) Logf(format string, args ...interface{}) {
	robotLogf(format, args...)
}

func (e storeMaintenanceEnv) RandBetween(min, max int) int {
	return e.manager.randBetween(min, max)
}

func (e storeMaintenanceEnv) RandomMap(maps []shared.MapCatalogItem, level int) (shared.MapCatalogItem, bool) {
	return e.manager.randomMap(maps, level)
}

func (e storeMaintenanceEnv) ResetPrivateStore(uid int) {
	if e.manager.doll != nil {
		robotruntime.ResetPrivateStore(e.manager.doll, uid)
	}
}

func (e storeMaintenanceEnv) RestoreDummyNormal(info robotcap.Info) error {
	return e.manager.schemaRepo().RestoreDummyNormal(info)
}

func (e storeMaintenanceEnv) RevokeStorePermission(uid, cid int) error {
	return e.manager.schemaRepo().RevokeStorePermission(uid, cid)
}

func (e storeMaintenanceEnv) SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error) {
	return e.manager.repo().SelectRobots(req)
}

func (e storeMaintenanceEnv) SyncCharacterVillage(cid int, village int) (int, error) {
	return e.manager.schemaRepo().SyncCharacterVillage(cid, village)
}
