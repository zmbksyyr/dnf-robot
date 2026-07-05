package robot

import (
	"fmt"
	"strings"
	"time"

	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/shared"
)

type CreateRequest struct {
	Count int `json:"count"`
}

type Info struct {
	UID     int    `json:"uid"`
	CID     int    `json:"cid"`
	Name    string `json:"name"`
	Level   int    `json:"level"`
	Job     int    `json:"job"`
	Grow    int    `json:"grow_type"`
	Port    int    `json:"port"`
	Village int    `json:"village"`
	Area    int    `json:"area"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
}

type StatusItem struct {
	UID              int               `json:"uid"`
	CID              int               `json:"cid"`
	Name             string            `json:"name"`
	Account          string            `json:"account"`
	DBState          string            `json:"db_state"`
	RobotState       shared.RobotState `json:"robot_state"`
	Operation        string            `json:"operation,omitempty"`
	ActorAttached    bool              `json:"actor_attached"`
	ActorSlot        int               `json:"actor_slot,omitempty"`
	ActorState       string            `json:"actor_state,omitempty"`
	ActorBusy        bool              `json:"actor_busy,omitempty"`
	ActorBusyKind    string            `json:"actor_busy_kind,omitempty"`
	Level            int               `json:"level"`
	Job              int               `json:"job"`
	Grow             int               `json:"grow"`
	State            int               `json:"state"`
	Online           bool              `json:"online"`
	DisconnectReason int               `json:"disconnect_reason"`
	Reconnects       int               `json:"reconnects"`
	RobotType        int               `json:"robot_type"`
	StoreDisplaySent bool              `json:"store_display_sent"`
	StoreDisplayAck  bool              `json:"store_display_ack"`
	StoreCreated     bool              `json:"store_created"`
	Village          int               `json:"village"`
	Area             int               `json:"area"`
	X                int               `json:"x"`
	Y                int               `json:"y"`
	UptimeSeconds    int               `json:"uptime_seconds"`
	MissingCore      bool              `json:"missing_core,omitempty"`
}

func UIDs(robots []Info) []int {
	out := make([]int, 0, len(robots))
	for _, r := range robots {
		out = append(out, r.UID)
	}
	return out
}

type CommandRequest struct {
	Count int   `json:"count"`
	UIDs  []int `json:"uids"`
}

func CommandRequestScope(req CommandRequest) string {
	if len(req.UIDs) > 0 {
		return fmt.Sprintf("uids=%d", len(req.UIDs))
	}
	return fmt.Sprintf("count=%d", req.Count)
}

type CleanupRequest struct {
	UIDs                    []int `json:"uids"`
	MinUID                  int   `json:"uid_min"`
	MaxUID                  int   `json:"uid_max"`
	Force                   bool  `json:"force"`
	InternalConfirmedBroken bool  `json:"-"`
}

func CleanupRequestScope(req CleanupRequest) string {
	if len(req.UIDs) > 0 {
		return fmt.Sprintf("uids=%d force=%v", len(req.UIDs), req.Force)
	}
	if req.MinUID > 0 || req.MaxUID > 0 {
		return fmt.Sprintf("range=%d-%d force=%v", req.MinUID, req.MaxUID, req.Force)
	}
	return fmt.Sprintf("all force=%v", req.Force)
}

type ActionResult struct {
	UID     int    `json:"uid"`
	CID     int    `json:"cid,omitempty"`
	OK      bool   `json:"ok"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type CommandResult struct {
	Requested int            `json:"requested"`
	Accepted  int            `json:"accepted"`
	Confirmed int            `json:"confirmed"`
	Failed    int            `json:"failed"`
	Robots    []ActionResult `json:"robots"`
}

func NewCommandResult(requested int) CommandResult {
	return CommandResult{Requested: requested, Robots: make([]ActionResult, 0, requested)}
}

func CommandOperationSummary(res CommandResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("requested=%d accepted=%d confirmed=%d failed=%d", res.Requested, res.Accepted, res.Confirmed, res.Failed)
}

type CleanupCandidate struct {
	UID       int    `json:"uid"`
	CID       int    `json:"cid"`
	Name      string `json:"name"`
	Account   string `json:"account"`
	Protected bool   `json:"protected"`
	Reason    string `json:"reason,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
}

type CleanupResult struct {
	DryRun     bool               `json:"dry_run"`
	Requested  int                `json:"requested"`
	Candidates []CleanupCandidate `json:"candidates"`
	Deleted    int                `json:"deleted"`
	Skipped    int                `json:"skipped"`
}

func CleanupOperationSummary(res CleanupResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("candidates=%d deleted=%d skipped=%d", len(res.Candidates), res.Deleted, res.Skipped)
}

func IsStructuralOperation(typ string) bool {
	switch strings.TrimSpace(typ) {
	case "create", "cleanup":
		return true
	default:
		return false
	}
}

type AutoStatus struct {
	Enabled           bool      `json:"enabled"`
	TargetOnline      int       `json:"target_online"`
	Actors            int       `json:"actors"`
	Leased            int       `json:"leased"`
	Idle              int       `json:"idle"`
	Recycling         int       `json:"recycling"`
	BlockedUIDs       int       `json:"blocked_uids"`
	ActorIdle         int       `json:"actor_idle"`
	ActorAssigned     int       `json:"actor_assigned"`
	ActorOnline       int       `json:"actor_online"`
	ActorRunning      int       `json:"actor_running"`
	ActorBusy         int       `json:"actor_busy"`
	ActorReleasing    int       `json:"actor_releasing"`
	Running           int       `json:"running"`
	Connecting        int       `json:"connecting"`
	GamePortReady     bool      `json:"game_port_ready"`
	GamePortAddress   string    `json:"game_port_address,omitempty"`
	GamePortStableAt  time.Time `json:"game_port_stable_at,omitempty"`
	StoreProbability  int       `json:"store_probability_percent"`
	StoreRunning      int       `json:"store_running"`
	Created           int       `json:"created"`
	OnlineSuccess     int       `json:"online_success"`
	OnlineFailed      int       `json:"online_failed"`
	MoveSuccess       int       `json:"move_success"`
	MoveFailed        int       `json:"move_failed"`
	ShoutLocalSuccess int       `json:"shout_local_success"`
	ShoutLocalFailed  int       `json:"shout_local_failed"`
	ShoutWorldSuccess int       `json:"shout_world_success"`
	ShoutWorldFailed  int       `json:"shout_world_failed"`
	StoreSuccess      int       `json:"store_success"`
	StoreFailed       int       `json:"store_failed"`
	StoreExpired      int       `json:"store_expired"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SchedulerStatus struct {
	Mode                    string    `json:"mode"`
	Reason                  string    `json:"reason"`
	RecentOperation         string    `json:"recent_operation,omitempty"`
	RecentOperationState    string    `json:"recent_operation_state,omitempty"`
	RecentOperationSummary  string    `json:"recent_operation_summary,omitempty"`
	TargetOnline            int       `json:"target_online"`
	Running                 int       `json:"running"`
	Connecting              int       `json:"connecting"`
	Actors                  int       `json:"actors"`
	Idle                    int       `json:"idle"`
	ActorIdle               int       `json:"actor_idle"`
	ActorAssigned           int       `json:"actor_assigned"`
	ActorOnline             int       `json:"actor_online"`
	ActorRunning            int       `json:"actor_running"`
	ActorBusy               int       `json:"actor_busy"`
	ActorReleasing          int       `json:"actor_releasing"`
	StoreRunning            int       `json:"store_running"`
	GamePortReady           bool      `json:"game_port_ready"`
	BreakerActive           bool      `json:"breaker_active"`
	CPUPercent              float64   `json:"cpu_percent"`
	MemoryMB                int       `json:"memory_mb"`
	Goroutines              int       `json:"goroutines"`
	OnlineBatchSize         int       `json:"online_batch_size"`
	OnlineStartRate         int       `json:"online_start_rate"`
	OnlineFillTimeoutSec    int       `json:"online_fill_timeout_sec"`
	MoveIntervalMinSec      int       `json:"move_interval_min_sec"`
	MoveIntervalMaxSec      int       `json:"move_interval_max_sec"`
	ShoutIntervalMinSec     int       `json:"shout_interval_min_sec"`
	ShoutIntervalMaxSec     int       `json:"shout_interval_max_sec"`
	StoreConcurrent         int       `json:"store_concurrent"`
	StoreProbabilityPercent int       `json:"store_probability_percent"`
	StoreIntervalMinSec     int       `json:"store_interval_min_sec"`
	StoreIntervalMaxSec     int       `json:"store_interval_max_sec"`
	StoreDurationSec        int       `json:"store_duration_sec"`
	StoreTickSec            int       `json:"store_tick_sec"`
	StoreMaxPositionTries   int       `json:"store_max_position_tries"`
	StoreFailCooldownSec    int       `json:"store_fail_cooldown_sec"`
	ScaleUpBatch            int       `json:"scale_up_batch"`
	ScaleDownBatch          int       `json:"scale_down_batch"`
	BreakerReleaseBatch     int       `json:"breaker_release_batch"`
	PortDownReleaseBatch    int       `json:"port_down_release_batch"`
	OperationActive         bool      `json:"operation_active"`
	Operation               string    `json:"operation,omitempty"`
	OperationStartedAt      time.Time `json:"operation_started_at,omitempty"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type OperationStatus struct {
	ID         int64     `json:"id"`
	Type       string    `json:"type"`
	Scope      string    `json:"scope,omitempty"`
	State      string    `json:"state"`
	Summary    string    `json:"summary,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const (
	OperationStateRunning = "running"
	OperationStateDone    = "done"
	OperationStateFailed  = "failed"
	OperationStateUnknown = "unknown"
)

type ConfigUpdateRequest struct {
	Text    string                 `json:"text"`
	Updates map[string]interface{} `json:"updates"`
}

type ConfigResult struct {
	Path   string                    `json:"path"`
	Text   string                    `json:"text"`
	Config robotconfig.RuntimeConfig `json:"config"`
}

type RuntimeStatus struct {
	UID                  int
	CID                  int
	State                int
	StateName            string
	LastError            int
	DisconnectReason     int
	Reconnects           int
	RunStartTime         int64
	UptimeSeconds        int
	RobotType            int
	StoreDisplaySent     bool
	StoreDisplayAck      bool
	StoreDisplayRejected bool
	StoreCreateRejected  bool
	LastStoreError       byte
	StoreCreated         bool
	Village              int
	Area                 int
	X                    int
	Y                    int
}

const (
	RuntimeStateStop    = "stop"
	RuntimeStateInit    = "init"
	RuntimeStateLogin   = "login"
	RuntimeStateRunning = "running"
	RuntimeStateClean   = "clean"
	RuntimeStateWrong   = "wrong"
	RuntimeStateUnknown = "unknown"
)

func StateName(state int) string {
	switch state {
	case 0:
		return RuntimeStateStop
	case 1:
		return RuntimeStateInit
	case 2:
		return RuntimeStateLogin
	case 3:
		return RuntimeStateRunning
	case 4:
		return RuntimeStateClean
	case 5:
		return RuntimeStateWrong
	default:
		return RuntimeStateUnknown
	}
}

func ActiveRuntimeStatus(st RuntimeStatus) bool {
	return st.StateName == RuntimeStateRunning && st.DisconnectReason == 0
}

func CopyRuntimeStatusMap(in map[int]RuntimeStatus) map[int]RuntimeStatus {
	out := make(map[int]RuntimeStatus, len(in))
	for uid, st := range in {
		out[uid] = st
	}
	return out
}

type RuntimeStatusSummary struct {
	Running    int
	Connecting int
	Stores     int
}

func SummarizeRuntimeStatusSlice(status []RuntimeStatus) RuntimeStatusSummary {
	var summary RuntimeStatusSummary
	for _, st := range status {
		summary.Add(st)
	}
	return summary
}

func SummarizeRuntimeStatusMap(status map[int]RuntimeStatus) RuntimeStatusSummary {
	var summary RuntimeStatusSummary
	for _, st := range status {
		summary.Add(st)
	}
	return summary
}

func (s *RuntimeStatusSummary) Add(st RuntimeStatus) {
	if st.DisconnectReason != 0 {
		return
	}
	switch st.StateName {
	case RuntimeStateRunning:
		s.Running++
		if (st.RobotType == 2 || st.RobotType == 3) && st.StoreDisplayAck {
			s.Stores++
		}
	case RuntimeStateInit, RuntimeStateLogin:
		s.Connecting++
	}
}
