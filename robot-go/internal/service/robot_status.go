package service

import (
	"strings"
	"time"
)

type RobotStatusItem struct {
	UID              int    `json:"uid"`
	CID              int    `json:"cid"`
	Name             string `json:"name"`
	Account          string `json:"account"`
	DBState          string `json:"db_state"`
	RuntimeState     string `json:"runtime_state"`
	DesiredState     string `json:"desired_state"`
	HealthState      string `json:"health_state"`
	Operation        string `json:"operation,omitempty"`
	ActorAttached    bool   `json:"actor_attached"`
	ActorSlot        int    `json:"actor_slot,omitempty"`
	ActorState       string `json:"actor_state,omitempty"`
	ActorBusy        bool   `json:"actor_busy,omitempty"`
	ActorBusyKind    string `json:"actor_busy_kind,omitempty"`
	Level            int    `json:"level"`
	Job              int    `json:"job"`
	Grow             int    `json:"grow"`
	State            int    `json:"state"`
	StateName        string `json:"state_name"`
	Online           bool   `json:"online"`
	DisconnectReason int    `json:"disconnect_reason"`
	Reconnects       int    `json:"reconnects"`
	RobotType        int    `json:"robot_type"`
	StoreDisplaySent bool   `json:"store_display_sent"`
	StoreDisplayAck  bool   `json:"store_display_ack"`
	StoreCreated     bool   `json:"store_created"`
	Village          int    `json:"village"`
	Area             int    `json:"area"`
	X                int    `json:"x"`
	Y                int    `json:"y"`
	UptimeSeconds    int    `json:"uptime_seconds"`
	MissingCore      bool   `json:"missing_core,omitempty"`
}

type RobotStatusResult struct {
	Robots    []RobotStatusItem `json:"robots"`
	Total     int               `json:"total"`
	Running   int               `json:"running"`
	Store     int               `json:"store"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func (m *RobotManager) RobotsStatus(req RobotCommandRequest) (RobotStatusResult, error) {
	runtime := m.runtimeStatusMap()
	actors := m.actorStatusMap()
	args := make([]interface{}, 0)
	where := ""
	limit := ""
	if len(req.UIDs) > 0 {
		holders := strings.TrimRight(strings.Repeat("?,", len(req.UIDs)), ",")
		where = "WHERE r.uid IN (" + holders + ")"
		for _, uid := range req.UIDs {
			args = append(args, uid)
		}
	} else {
		if req.Count <= 0 {
			req.Count = 500
		}
		limit = " LIMIT ?"
		args = append(args, req.Count)
	}
	query := `
SELECT r.uid,r.cid,IFNULL(r.charac_name,''),IFNULL(r.account,''),IFNULL(c.lev,0),IFNULL(c.job,0),IFNULL(c.grow_type,0),
       IF(c.charac_no IS NULL,0,1),
       IFNULL(d.curvill,0),IFNULL(d.curarea,0),IFNULL(d.curx,0),IFNULL(d.cury,0)
FROM d_starsky.robot_registry r
LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=r.cid AND c.delete_flag=0
LEFT JOIN d_starsky.Dummylist d ON CAST(d.UID AS UNSIGNED)=r.uid
` + where + `
ORDER BY r.uid` + limit
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return RobotStatusResult{}, err
	}
	defer rows.Close()

	out := RobotStatusResult{UpdatedAt: time.Now()}
	for rows.Next() {
		var item RobotStatusItem
		var coreAlive int
		if err := rows.Scan(&item.UID, &item.CID, &item.Name, &item.Account, &item.Level, &item.Job, &item.Grow, &coreAlive, &item.Village, &item.Area, &item.X, &item.Y); err != nil {
			return RobotStatusResult{}, err
		}
		item.DBState = "exists"
		item.RuntimeState = "offline"
		item.DesiredState = "unmanaged"
		item.HealthState = "ok"
		if coreAlive == 0 {
			item.MissingCore = true
			item.DBState = "missing_core"
			item.RuntimeState = "unknown"
			item.StateName = "broken"
			item.HealthState = "broken"
		} else if st, ok := runtime[item.UID]; ok {
			item.State = st.State
			item.StateName = st.StateName
			item.RuntimeState = st.StateName
			item.Online = activeRuntimeStatus(st)
			item.DisconnectReason = st.DisconnectReason
			item.Reconnects = st.Reconnects
			item.RobotType = st.RobotType
			item.StoreDisplaySent = st.StoreDisplaySent
			item.StoreDisplayAck = st.StoreDisplayAck
			item.StoreCreated = st.StoreCreated
			item.UptimeSeconds = st.UptimeSeconds
			if st.Village != 0 || st.Area != 0 || st.X != 0 || st.Y != 0 {
				item.Village = st.Village
				item.Area = st.Area
				item.X = st.X
				item.Y = st.Y
			}
			if st.DisconnectReason != 0 {
				item.HealthState = "disconnected"
			}
		} else {
			item.StateName = "offline"
		}
		if actor, ok := actors[item.UID]; ok {
			item.ActorAttached = true
			item.ActorSlot = actor.SlotID
			item.ActorState = string(actor.State)
			item.ActorBusy = actor.Busy
			item.ActorBusyKind = actor.BusyKind
			if actor.OnlineDesired {
				item.DesiredState = "online"
			} else {
				item.DesiredState = "offline"
			}
			item.Operation = actorOperation(actor)
			item.HealthState = actorHealthState(item.HealthState, actor)
		}
		if item.Online {
			out.Running++
		}
		if item.RobotType == 2 || item.RobotType == 3 || item.StoreCreated || item.StoreDisplayAck {
			out.Store++
		}
		out.Robots = append(out.Robots, item)
	}
	if err := rows.Err(); err != nil {
		return RobotStatusResult{}, err
	}
	out.Total = len(out.Robots)
	return out, nil
}

func actorOperation(actor robotActorSnapshot) string {
	if actor.BusyKind != "" {
		return actor.BusyKind
	}
	switch actor.State {
	case robotActorAssigned, robotActorOnline:
		return "online"
	case robotActorReleasing:
		return "release"
	case robotActorOffline:
		return "offline"
	default:
		return ""
	}
}

func actorHealthState(current string, actor robotActorSnapshot) string {
	if actor.Failures > 0 {
		return "suspect"
	}
	if current != "" {
		return current
	}
	return "ok"
}

func (m *RobotManager) actorStatusMap() map[int]robotActorSnapshot {
	out := map[int]robotActorSnapshot{}
	supervisor := m.currentSupervisor()
	if supervisor == nil {
		return out
	}
	for _, snap := range supervisor.actorSnapshots() {
		if snap.UID > 0 {
			out[snap.UID] = snap
		}
	}
	return out
}
