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
	db               *sql.DB
	cfg              *config.SysConfig
	doll             *DollService
	mu               sync.Mutex
	autoMu           sync.Mutex
	cacheMu          sync.Mutex
	randMu           sync.Mutex
	storeSlotMu      sync.Mutex
	colCache         map[string]map[string]bool
	rand             *rand.Rand
	autoStoreBusy    map[int]bool
	autoStoreSlots   chan struct{}
	autoStoreCap     int
	autoEnabled      bool
	autoPortSince    time.Time
	autoPortReady    bool
	autoPortLog      time.Time
	autoStats        RobotAutoStatus
	configCache      robotRuntimeConfig
	configMod        time.Time
	configCached     bool
	shoutCache       shoutTemplates
	shoutMod         time.Time
	shoutCached      bool
	mapCache         []mapCatalogItem
	mapMod           time.Time
	mapCached        bool
	equipCache       []equipmentCatalogItem
	equipMod         time.Time
	equipCached      bool
	stackCache       []equipmentCatalogItem
	stackMod         time.Time
	stackCached      bool
	supervisor       *RobotSupervisor
	storePointsCoord *storePointCoordinator
}

func NewRobotManager(db *sql.DB, cfg *config.SysConfig, doll *DollService) *RobotManager {
	return &RobotManager{
		db:       db,
		cfg:      cfg,
		doll:     doll,
		colCache: make(map[string]map[string]bool),
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
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
