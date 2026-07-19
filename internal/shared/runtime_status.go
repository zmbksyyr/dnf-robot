package shared

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
	DisjointCreateSent   bool
	DisjointDirectAck    bool
	DisjointActive       bool
	LastDisjointError    byte
	PartyActive          bool
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
		if (st.RobotType == 2 && st.StoreDisplayAck) || (st.RobotType == 3 && st.DisjointActive) {
			s.Stores++
		}
	case RuntimeStateInit, RuntimeStateLogin:
		s.Connecting++
	}
}
