package service

import (
	"database/sql"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"robot/internal/config"
)

type RobotManager struct {
	db                              *sql.DB
	cfg                             *config.SysConfig
	doll                            *DollService
	startedAt                       time.Time
	mu                              sync.Mutex
	lifecycleMu                     sync.Mutex
	autoMu                          sync.Mutex
	runtimeStatusMu                 sync.Mutex
	cacheMu                         sync.Mutex
	randMu                          sync.Mutex
	storeSlotMu                     sync.Mutex
	operationMu                     sync.Mutex
	cleanupMu                       sync.Mutex
	colCache                        map[string]map[string]bool
	rand                            *rand.Rand
	autoStoreBusy                   map[int]bool
	cleanupPendingUIDs              map[int]time.Time
	autoStoreSlots                  chan struct{}
	autoStoreCap                    int
	autoEnabled                     bool
	autoPortSince                   time.Time
	autoPortReady                   bool
	autoPortLog                     time.Time
	autoStats                       RobotAutoStatus
	autoBreakerUntil                time.Time
	autoBreakerReason               string
	autoBreakerLastCheck            time.Time
	autoBreakerLastOnlineFailed     int
	autoBreakerLastMoveFailed       int
	autoBreakerLastShoutLocalFailed int
	autoBreakerLastShoutWorldFailed int
	autoBreakerLastStoreFailed      int
	runtimeStatusCache              map[int]RuntimeRobotStatus
	runtimeStatusCacheAt            time.Time
	autoPolicyLastMode              schedulerPolicyMode
	autoPolicyLastReason            string
	schedulerStatus                 SchedulerStatus
	nextOperationID                 int64
	operations                      []RobotOperationStatus
	structuralOp                    string
	structuralOpStarted             time.Time
	actorContainerOp                string
	actorContainerOpStarted         time.Time
	configCache                     robotRuntimeConfig
	configMod                       time.Time
	configCached                    bool
	shoutCache                      shoutTemplates
	shoutMod                        time.Time
	shoutCached                     bool
	mapCache                        []mapCatalogItem
	mapMod                          time.Time
	mapCached                       bool
	equipCache                      []equipmentCatalogItem
	equipMod                        time.Time
	equipCached                     bool
	stackCache                      []equipmentCatalogItem
	stackMod                        time.Time
	stackCached                     bool
	supervisor                      *RobotSupervisor
	storePointsCoord                *storePointCoordinator
}

func (m *RobotManager) beginStructuralOp(op string) func() {
	if strings.TrimSpace(op) == "" {
		op = "unknown"
	}
	m.autoMu.Lock()
	m.structuralOp = op
	m.structuralOpStarted = time.Now()
	m.autoMu.Unlock()
	robotLogf("[RobotLifecycle] op=%s state=begin\n", op)
	return func() {
		m.autoMu.Lock()
		if m.structuralOp == op {
			m.structuralOp = ""
			m.structuralOpStarted = time.Time{}
		}
		m.autoMu.Unlock()
		robotLogf("[RobotLifecycle] op=%s state=end\n", op)
	}
}

func (m *RobotManager) structuralOperation() (string, time.Time, bool) {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if m.structuralOp != "" && (m.structuralOpStarted.IsZero() || time.Since(m.structuralOpStarted) > 10*time.Minute) {
		robotLogf("[RobotLifecycle] op=%s state=expired started=%s\n", m.structuralOp, m.structuralOpStarted.Format(time.RFC3339))
		m.structuralOp = ""
		m.structuralOpStarted = time.Time{}
		return "", time.Time{}, false
	}
	return m.structuralOp, m.structuralOpStarted, m.structuralOp != ""
}

func (m *RobotManager) beginActorContainerOp(op string) func() {
	if strings.TrimSpace(op) == "" {
		op = "actor_container"
	}
	m.autoMu.Lock()
	m.actorContainerOp = op
	m.actorContainerOpStarted = time.Now()
	m.autoMu.Unlock()
	robotLogf("[RobotLifecycle] actor_container op=%s state=begin\n", op)
	return func() {
		m.autoMu.Lock()
		if m.actorContainerOp == op {
			m.actorContainerOp = ""
			m.actorContainerOpStarted = time.Time{}
		}
		m.autoMu.Unlock()
		robotLogf("[RobotLifecycle] actor_container op=%s state=end\n", op)
	}
}

func (m *RobotManager) actorContainerOperation() (string, time.Time, bool) {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.actorContainerOp, m.actorContainerOpStarted, m.actorContainerOp != ""
}

func NewRobotManager(db *sql.DB, cfg *config.SysConfig, doll *DollService) *RobotManager {
	return &RobotManager{
		db:                 db,
		cfg:                cfg,
		doll:               doll,
		startedAt:          time.Now(),
		colCache:           make(map[string]map[string]bool),
		rand:               rand.New(rand.NewSource(time.Now().UnixNano())),
		cleanupPendingUIDs: make(map[int]time.Time),
	}
}

func (m *RobotManager) robotConnectIP() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.RobotConnectIP) != "" {
		return strings.TrimSpace(m.cfg.RobotConnectIP)
	}
	if m.cfg != nil {
		return strings.TrimSpace(m.cfg.RobotInnerIP)
	}
	return ""
}

func (m *RobotManager) robotGamePortAddress() string {
	port := 10011
	if m.cfg != nil && m.cfg.RobotGamePort > 0 {
		port = m.cfg.RobotGamePort
	}
	host := strings.TrimSpace(m.robotConnectIP())
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
