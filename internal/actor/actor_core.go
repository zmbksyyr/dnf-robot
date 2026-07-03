package actor

import (
	"math/rand"
	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/lockhub"
	"sync"
	"time"
)

// ---- core.go ----
type Actor struct {
	slotID  int
	uid     int
	mode    Mode
	state   State
	runtime RobotRuntime
	rand    *rand.Rand
	mu      lockhub.Locker
	cmds    chan request
	ctrls   chan control
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

func NewActor(slotID int, mode Mode, runtime RobotRuntime) *Actor {
	return &Actor{
		slotID:  slotID,
		mode:    mode,
		state:   StateIdle,
		runtime: runtime,
		rand:    rand.New(rand.NewSource(time.Now().UnixNano() + int64(slotID)*7919)),
		cmds:    make(chan request, 16),
		ctrls:   make(chan control, 4),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// ---- model.go ----
type RobotRuntime interface {
	Config() robotconfig.RuntimeConfig
	Status(uid int) (robotcap.RuntimeStatus, bool)
	IsActive(uid int) bool
	FinishStoreState(uid, cid int, reason string)
	AddAutoOnline(success, failed int)
	AutoActionsEnabled(rc robotconfig.RuntimeConfig) bool
	RandomShoutMessage(randIntn func(int) int) string
	OnlineNoConfirm(uid int) robotcap.ActionResult
	Logout(uid int) robotcap.ActionResult
	Move(uid int) robotcap.ActionResult
	Shout(uid int, world bool) robotcap.ActionResult
	Store(uid int) robotcap.ActionResult
	AutoMove(uid int) robotcap.ActionResult
	AutoShout(uid int, world bool, msg string) robotcap.ActionResult
	AutoStore(uid int, shouldStop func() bool) robotcap.ActionResult
	ExpireStore(uid int) robotcap.ActionResult
}

type Mode int

const (
	ModeAuto Mode = iota
)

type Command int

const (
	CommandMove Command = iota
	CommandShoutLocal
	CommandShoutWorld
	CommandStore
	CommandOnline
	CommandLogout
)

type ControlKind int

const (
	ControlAssign ControlKind = iota
	ControlRelease
)

type State string

const (
	StateIdle      State = "idle"
	StateOffline   State = "attached_offline"
	StateAssigned  State = "assigned"
	StateOnline    State = "online"
	StateRunning   State = "running"
	StateBusy      State = "busy"
	StateReleasing State = "releasing"
)

type Snapshot struct {
	SlotID         int
	UID            int
	Mode           Mode
	State          State
	Busy           bool
	BusyKind       string
	OnlineDesired  bool
	LastOnlineTry  time.Time
	FirstFailureAt time.Time
	Failures       int
}

type Health string

const (
	HealthHealthy   Health = "healthy"
	HealthIdle      Health = "idle"
	HealthBusy      Health = "busy"
	HealthUnhealthy Health = "unhealthy"
)

type Status struct {
	Snapshot
	Health       Health
	HealthReason string
	RecycleUID   bool
}

func SnapshotEmpty(s Snapshot) bool {
	return s.UID <= 0
}

func SnapshotSchedulerPending(s Snapshot) bool {
	if SnapshotEmpty(s) {
		return true
	}
	switch s.State {
	case StateIdle, StateOffline, StateAssigned, StateOnline, StateReleasing:
		return true
	default:
		return false
	}
}

func Operation(s Snapshot) string {
	if s.BusyKind != "" {
		return s.BusyKind
	}
	switch s.State {
	case StateAssigned, StateOnline:
		return "online"
	case StateReleasing:
		return "release"
	case StateOffline:
		return "offline"
	default:
		return ""
	}
}

func HealthState(current string, s Snapshot) string {
	if s.Failures > 0 {
		return "suspect"
	}
	if current != "" {
		return current
	}
	return "ok"
}

func StopPriority(uid int, status map[int]robotcap.RuntimeStatus) int {
	if uid <= 0 {
		return 0
	}
	st, ok := status[uid]
	if !ok || st.DisconnectReason != 0 || st.StateName == "init" || st.StateName == "login" {
		return 1
	}
	if st.RobotType == 2 || st.RobotType == 3 || st.StoreDisplayAck {
		return 2
	}
	return 3
}

func RandomizedDue(next *time.Time, now time.Time, minSec, maxSec int, randBetween func(int, int) int) bool {
	if minSec <= 0 || maxSec <= 0 || randBetween == nil {
		return false
	}
	if maxSec < minSec {
		minSec, maxSec = maxSec, minSec
	}
	if next.IsZero() {
		*next = now.Add(time.Duration(randBetween(minSec, maxSec)) * time.Second)
		return false
	}
	if now.Before(*next) {
		return false
	}
	*next = now.Add(time.Duration(randBetween(minSec, maxSec)) * time.Second)
	return true
}

type StatusConfig struct {
	BadFailures            int
	OnlineConfirmTimeoutMS int
}

type RuntimeStatusLookup func(uid int) (robotcap.RuntimeStatus, bool)

func EvaluateStatus(snapshot Snapshot, now time.Time, cfg StatusConfig, lookup RuntimeStatusLookup) Status {
	status := Status{Snapshot: snapshot, Health: HealthHealthy}
	if snapshot.Mode != ModeAuto || snapshot.UID <= 0 {
		status.Health = HealthIdle
		return status
	}
	if snapshot.Busy {
		status.Health = HealthBusy
		status.HealthReason = snapshot.BusyKind
		return status
	}
	if snapshot.Failures >= cfg.BadFailures {
		status.Health = HealthUnhealthy
		status.HealthReason = "failure_count"
		status.RecycleUID = true
		return status
	}
	if snapshot.State == StateOnline && !snapshot.LastOnlineTry.IsZero() {
		timeout := time.Duration(cfg.OnlineConfirmTimeoutMS) * time.Millisecond
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		if now.Sub(snapshot.LastOnlineTry) > timeout {
			if lookup == nil {
				status.Health = HealthUnhealthy
				status.HealthReason = "online_confirm_timeout"
				return status
			}
			if st, ok := lookup(snapshot.UID); !ok || st.StateName != "running" || st.DisconnectReason != 0 {
				status.Health = HealthUnhealthy
				status.HealthReason = "online_confirm_timeout"
				return status
			}
		}
	}
	return status
}
