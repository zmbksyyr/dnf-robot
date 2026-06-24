package service

import (
	"math/rand"
	"sync"
	"time"
)

type robotActorMode int

const (
	robotActorAuto robotActorMode = iota
)

type robotActorCommand int

const (
	robotActorMove robotActorCommand = iota
	robotActorShoutLocal
	robotActorShoutWorld
	robotActorStore
	robotActorCmdOnline
	robotActorCmdLogout
)

type robotActorState string

const (
	robotActorIdle      robotActorState = "idle"
	robotActorOffline   robotActorState = "attached_offline"
	robotActorAssigned  robotActorState = "assigned"
	robotActorOnline    robotActorState = "online"
	robotActorRunning   robotActorState = "running"
	robotActorBusy      robotActorState = "busy"
	robotActorReleasing robotActorState = "releasing"
)

type robotActorRequest struct {
	cmd  robotActorCommand
	done chan RobotActionResult
}

type robotActorControlKind int

const (
	robotActorAssign robotActorControlKind = iota
	robotActorRelease
)

type robotActorControl struct {
	kind robotActorControlKind
	uid  int
	done chan robotActorControlResult
}

type robotActorControlResult struct {
	uid int
	ok  bool
}

type robotActorSnapshot struct {
	SlotID         int
	UID            int
	Mode           robotActorMode
	State          robotActorState
	Busy           bool
	BusyKind       string
	OnlineDesired  bool
	LastOnlineTry  time.Time
	FirstFailureAt time.Time
	Failures       int
}

type robotActorHealth string

const (
	robotActorHealthHealthy   robotActorHealth = "healthy"
	robotActorHealthIdle      robotActorHealth = "idle"
	robotActorHealthBusy      robotActorHealth = "busy"
	robotActorHealthUnhealthy robotActorHealth = "unhealthy"
)

type robotActorStatus struct {
	robotActorSnapshot
	Health       robotActorHealth
	HealthReason string
	RecycleUID   bool
}

type robotActor struct {
	slotID  int
	uid     int
	mode    robotActorMode
	state   robotActorState
	runtime *RobotRuntime
	rand    *rand.Rand
	mu      sync.Mutex
	cmds    chan robotActorRequest
	ctrls   chan robotActorControl
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once

	nextMove         time.Time
	nextLocalShout   time.Time
	nextWorldShout   time.Time
	nextStore        time.Time
	storeUntil       time.Time
	lastOnlineTry    time.Time
	firstFailureAt   time.Time
	failures         int
	busy             bool
	busyKind         string
	releaseRequested bool
	onlineDesired    bool
}

func newRobotActor(slotID int, mode robotActorMode, runtime *RobotRuntime) *robotActor {
	return &robotActor{
		slotID:  slotID,
		mode:    mode,
		state:   robotActorIdle,
		runtime: runtime,
		rand:    rand.New(rand.NewSource(time.Now().UnixNano() + int64(slotID)*7919)),
		cmds:    make(chan robotActorRequest, 16),
		ctrls:   make(chan robotActorControl, 4),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (a *robotActor) start() {
	go a.loop()
}
