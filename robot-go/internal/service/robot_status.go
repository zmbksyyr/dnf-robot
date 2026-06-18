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
       IFNULL(d.curvill,0),IFNULL(d.curarea,0),IFNULL(d.curx,0),IFNULL(d.cury,0)
FROM d_starsky.robot_registry r
LEFT JOIN taiwan_cain.charac_info c ON c.charac_no=r.cid
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
		if err := rows.Scan(&item.UID, &item.CID, &item.Name, &item.Account, &item.Level, &item.Job, &item.Grow, &item.Village, &item.Area, &item.X, &item.Y); err != nil {
			return RobotStatusResult{}, err
		}
		if st, ok := runtime[item.UID]; ok {
			item.State = st.State
			item.StateName = st.StateName
			item.Online = st.StateName == "running" && st.DisconnectReason == 0
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
		} else {
			item.StateName = "offline"
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
