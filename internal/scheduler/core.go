package scheduler

import (
	"errors"
	"math/rand"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	lifecyclecap "robot/internal/capability/robotlifecycle"
	storecap "robot/internal/capability/store"
	"robot/internal/foundation/config"
	"robot/internal/foundation/dbstatus"
	"robot/internal/foundation/lockhub"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
	"sync/atomic"
	"time"
)

type RobotManager struct {
	database                        dbstatus.Database
	cfg                             *config.SysConfig
	doll                            Runtime
	worldShout                      WorldShout
	locks                           *lockhub.Hub
	startedAt                       time.Time
	autoMu                          lockhub.Locker
	sessionMu                       lockhub.Locker
	rand                            *rand.Rand
	sessionLastLogout               map[int]time.Time
	sessionReloginDelay             time.Duration
	autoStoreBusy                   map[int]bool
	autoStoreItemPending            int
	autoStoreDisjointPending        int
	cleanupPendingUIDs              map[int]time.Time
	autoStoreSlots                  chan struct{}
	autoStoreCap                    int
	autoItemStoreSlots              chan struct{}
	autoItemStoreCap                int
	autoEnabled                     bool
	autoPortSince                   time.Time
	autoPortReady                   bool
	autoPortLog                     time.Time
	autoStats                       robotcap.AutoStatus
	autoBreakerUntil                time.Time
	autoBreakerReason               string
	autoBreakerLastCheck            time.Time
	autoBreakerLastOnlineFailed     int
	autoBreakerLastMoveFailed       int
	autoBreakerLastShoutLocalFailed int
	autoBreakerLastShoutWorldFailed int
	autoBreakerLastStoreFailed      int
	runtimeStatusMu                 lockhub.RWLocker
	runtimeStatusCache              map[int]robotcap.RuntimeStatus
	runtimeStatusCacheAt            time.Time
	runtimeStatusSummary            robotcap.RuntimeStatusSummary
	runtimeStatusSummaryAt          time.Time
	runtimeStatusRefresh            chan struct{}
	schedulerStatus                 robotcap.SchedulerStatus
	nextOperationID                 int64
	operations                      []robotcap.OperationStatus
	structuralOp                    string
	structuralOpStarted             time.Time
	actorContainerOp                string
	actorContainerOpStarted         time.Time
	configSnapshot                  atomic.Pointer[robotConfigSnapshot]
	supervisor                      *RobotSupervisor
	storePointsCoord                *storecap.PointCoordinator
	positionWrites                  *positionBatcher
	repository                      SchedulerRepository
	autoPolicy                      AutoPolicy
}

func NewRobotManager(database dbstatus.Database, cfg *config.SysConfig, doll Runtime) *RobotManager {
	if doll == nil {
		doll = noopRuntime{}
	}
	manager := &RobotManager{
		database:            database,
		cfg:                 cfg,
		doll:                doll,
		worldShout:          noopWorldShout{},
		locks:               lockhub.New(),
		startedAt:           time.Now(),
		rand:                rand.New(rand.NewSource(time.Now().UnixNano())),
		cleanupPendingUIDs:  make(map[int]time.Time),
		sessionLastLogout:   make(map[int]time.Time),
		sessionReloginDelay: 15 * time.Second,
	}
	manager.positionWrites = newPositionBatcher(manager.positionRepo(), defaultPositionBatchOptions())
	return manager
}

func (m *RobotManager) repo() SchedulerRepository {
	if m.repository != nil {
		return m.repository
	}
	if repository, ok := m.database.(SchedulerRepository); ok {
		return repository
	}
	return missingRepository{}
}

func (m *RobotManager) schemaRepo() SchemaRepository {
	if repository, ok := m.database.(SchemaRepository); ok {
		return repository
	}
	return missingSchemaRepository{}
}

func (m *RobotManager) positionRepo() robotPositionWriter {
	if repository, ok := m.database.(robotPositionWriter); ok {
		return repository
	}
	return missingPositionRepository{}
}

func (m *RobotManager) lockHub() *lockhub.Hub {
	if m.locks == nil {
		m.locks = lockhub.New()
	}
	return m.locks
}

func (m *RobotManager) withCache(reason string, fn func()) {
	_ = m.lockHub().WithResource(lockScopeConfig, lockResourceRobotConfig, reason, func() error {
		fn()
		return nil
	})
}

func (m *RobotManager) policy() AutoPolicy {
	if m.autoPolicy != nil {
		return m.autoPolicy
	}
	return defaultAutoPolicy{}
}

func (m *RobotManager) SetRepository(repository SchedulerRepository) {
	m.repository = repository
}

func (m *RobotManager) SetAutoPolicy(policy AutoPolicy) {
	m.autoPolicy = policy
}

type AutoPolicy interface {
	ApplyConfig(rc *robotconfig.RuntimeConfig, sig adaptiveSchedulerSignals) schedulerPolicyDecision
}

type defaultAutoPolicy struct{}

func (defaultAutoPolicy) ApplyConfig(rc *robotconfig.RuntimeConfig, sig adaptiveSchedulerSignals) schedulerPolicyDecision {
	return applyAdaptiveSchedulerConfig(rc, sig)
}

type SchedulerRepository interface {
	SelectRobots(req robotcap.CommandRequest) ([]robotcap.Info, error)
	EnsureSchema() error
}

type SchemaRepository interface {
	InsertIgnore(table string, values map[string]interface{}) error
	InsertIgnoreIfTableExists(table string, values map[string]interface{}) error
	TableColumns(table string) (map[string]bool, error)
	TableExists(table string) (bool, error)
	DeleteByIntIfTableExists(table, col string, id int) error
	NextInt(query string, fallback int) (int, error)
	AvailableRobotUIDs(count, start, end int) ([]int, error)
	AccountAutoIncrement() (int, error)
	AllocateRobotIDs(count, uidStart, uidEnd int) (lifecyclecap.RobotIDAllocation, error)
	CharacterNameExists(dbName string) (bool, error)
	EnsureAccount(uid int, innerIP string) error
	ClearTradePunish(uid int) (int64, error)
	CreateBaseCharacter(info robotcap.Info, rc robotconfig.RuntimeConfig) error
	SaveEquipmentSlots(cid int, raw []byte) error
	ReplaceAvatarItems(cid int, selected map[int]shared.EquipmentCatalogItem) error
	FollowAccountVillage(account string) (int, bool, error)
	MarkStoreStarted(uid int) error
	PrepareStorePosition(info robotcap.Info) error
	PrepareDisjointPosition(info robotcap.Info, cost int) error
	RestoreDummyNormal(info robotcap.Info) error
	SyncCharacterVillage(cid int, village int) (int, error)
	LoadInventory(cid int) ([]byte, error)
	SaveInventory(cid int, capacity int, raw []byte) error
	SaveInventoryRaw(cid int, raw []byte) error
	ReplaceStoreStall(uid int, title string, items []storecap.StallItem) (storecap.StallResult, error)
	RobotCID(uid int) (int, error)
	EnsureStorePermission(uid, cid int) (storecap.PermissionStatus, error)
	EnsureDisjointProfession(info robotcap.Info) error
	RepairRobotExpBounds(uid, cid int) (storecap.ExpRepairResult, error)
	RevokeStorePermission(uid, cid int) error
	FollowAccountUIDs(account string) ([]int, error)
	FollowAccountVillageLastPlayed(account string) (int, bool, error)
	RobotCharacterName(uid int) (string, error)
	AliveRobotUIDs(uids []int) (map[int]bool, error)
	RobotStatusRows(req robotcap.CommandRequest) ([]robotcap.StatusItem, error)
	CleanupCandidates(req robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, error)
	BatchDeleteRobotData(uids, cids []int) error
	UpsertDummy(info robotcap.Info, innerIP string) error
	RegisterRobot(info robotcap.Info) error
	RebuildCharacView(uid int) error
	CopyTemplateDefaults(cid int) error
}

type Runtime interface {
	SessionRuntime
	MoveRuntime
	ShoutRuntime
	StatusRuntime
	StoreRuntime
	AreaRuntime
}

type SessionRuntime interface {
	Logout(uid int) error
	Online(users []shared.RuntimeOnlineUser) error
}

type MoveRuntime interface {
	Move(command shared.RuntimeMoveCommand) error
}

type ShoutRuntime interface {
	Shout(command shared.RuntimeShoutCommand) error
}

type StatusRuntime interface {
	RuntimeStatus() []robotcap.RuntimeStatus
	PartyActive(uid int) bool
}

type StoreRuntime interface {
	StartPrivateStore(uid int, title string) bool
	ResetPrivateStore(uid int) bool
	StartDisjointStore(uid int, cost uint32) bool
	ResetDisjointStore(uid int) bool
	SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool
	CompletePrivateStoreDisplay(uid int) bool
}

type AreaRuntime interface {
	SetArea(uid int, village, area int, x, y int) bool
}

type missingRepository struct{}

func (missingRepository) SelectRobots(robotcap.CommandRequest) ([]robotcap.Info, error) {
	return nil, errors.New("scheduler repository is not configured")
}

func (missingRepository) EnsureSchema() error {
	return errors.New("scheduler repository is not configured")
}

type missingSchemaRepository struct{}

func (missingSchemaRepository) InsertIgnore(string, map[string]interface{}) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) InsertIgnoreIfTableExists(string, map[string]interface{}) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) TableColumns(string) (map[string]bool, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) TableExists(string) (bool, error) {
	return false, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) DeleteByIntIfTableExists(string, string, int) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) NextInt(string, int) (int, error) {
	return 0, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) AvailableRobotUIDs(int, int, int) ([]int, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) AccountAutoIncrement() (int, error) {
	return 0, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) AllocateRobotIDs(int, int, int) (lifecyclecap.RobotIDAllocation, error) {
	return lifecyclecap.RobotIDAllocation{}, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) CharacterNameExists(string) (bool, error) {
	return false, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) EnsureAccount(int, string) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) ClearTradePunish(int) (int64, error) {
	return 0, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) CreateBaseCharacter(robotcap.Info, robotconfig.RuntimeConfig) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) SaveEquipmentSlots(int, []byte) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) ReplaceAvatarItems(int, map[int]shared.EquipmentCatalogItem) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) FollowAccountVillage(string) (int, bool, error) {
	return 0, false, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) MarkStoreStarted(int) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) PrepareStorePosition(robotcap.Info) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) PrepareDisjointPosition(robotcap.Info, int) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RestoreDummyNormal(robotcap.Info) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) SyncCharacterVillage(int, int) (int, error) {
	return 0, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) LoadInventory(int) ([]byte, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) SaveInventory(int, int, []byte) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) SaveInventoryRaw(int, []byte) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) ReplaceStoreStall(int, string, []storecap.StallItem) (storecap.StallResult, error) {
	return storecap.StallResult{}, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RobotCID(int) (int, error) {
	return 0, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) EnsureStorePermission(int, int) (storecap.PermissionStatus, error) {
	return storecap.PermissionStatus{}, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) EnsureDisjointProfession(robotcap.Info) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RepairRobotExpBounds(int, int) (storecap.ExpRepairResult, error) {
	return storecap.ExpRepairResult{}, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RevokeStorePermission(int, int) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) FollowAccountUIDs(string) ([]int, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

type missingPositionRepository struct{}

func (missingPositionRepository) UpdateRobotPositions([]robotcap.PositionUpdate) error {
	return errors.New("scheduler position repository is not configured")
}

func (missingSchemaRepository) FollowAccountVillageLastPlayed(string) (int, bool, error) {
	return 0, false, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RobotCharacterName(int) (string, error) {
	return "", errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) AliveRobotUIDs([]int) (map[int]bool, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RobotStatusRows(robotcap.CommandRequest) ([]robotcap.StatusItem, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) CleanupCandidates(robotcap.CleanupRequest) ([]robotcap.CleanupCandidate, error) {
	return nil, errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) BatchDeleteRobotData([]int, []int) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) UpsertDummy(robotcap.Info, string) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RegisterRobot(robotcap.Info) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) RebuildCharacView(int) error {
	return errors.New("scheduler schema repository is not configured")
}

func (missingSchemaRepository) CopyTemplateDefaults(int) error {
	return errors.New("scheduler schema repository is not configured")
}

type noopRuntime struct{}

func (noopRuntime) Logout(int) error {
	return nil
}

func (noopRuntime) Move(shared.RuntimeMoveCommand) error {
	return nil
}

func (noopRuntime) Shout(shared.RuntimeShoutCommand) error {
	return nil
}

func (noopRuntime) Online([]shared.RuntimeOnlineUser) error {
	return nil
}

func (noopRuntime) RuntimeStatus() []robotcap.RuntimeStatus {
	return nil
}

func (noopRuntime) PartyActive(int) bool { return false }

func (noopRuntime) StartPrivateStore(uid int, title string) bool {
	return false
}

func (noopRuntime) ResetPrivateStore(uid int) bool {
	return false
}

func (noopRuntime) StartDisjointStore(uid int, cost uint32) bool {
	return false
}

func (noopRuntime) ResetDisjointStore(uid int) bool {
	return false
}

func (noopRuntime) SetArea(uid int, village, area int, x, y int) bool {
	return false
}

func (noopRuntime) SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool {
	return false
}

func (noopRuntime) CompletePrivateStoreDisplay(uid int) bool {
	return false
}

type WorldShout interface {
	SendWorldShout(msg, name string, senderID uint16) error
	SendMonitorAnnouncement(kind, msg, name string, senderID uint16) error
}

type noopWorldShout struct{}

func (noopWorldShout) SendWorldShout(msg, name string, senderID uint16) error {
	return nil
}

func (noopWorldShout) SendMonitorAnnouncement(kind, msg, name string, senderID uint16) error {
	return nil
}

func (m *RobotManager) SetWorldShout(worldShout WorldShout) {
	if worldShout == nil {
		worldShout = noopWorldShout{}
	}
	m.worldShout = worldShout
}

func robotLogf(format string, args ...interface{}) {
	foundationlog.Robotf(format, args...)
}
