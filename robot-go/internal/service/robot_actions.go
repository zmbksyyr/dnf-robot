package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"robot/internal/dnf"
)

func (m *RobotManager) Move(req RobotCommandRequest) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	rc := m.loadRobotConfig()
	maps := m.loadMapCatalog()
	status := m.runtimeStatusMap()
	result := newCommandResult(len(robots))
	type movePlan struct {
		robot  RobotInfo
		steps  int
		target [2]int
		speeds []int
	}
	var plans []movePlan
	for _, r := range robots {
		if st, ok := status[r.UID]; ok && activeRuntimeStatus(st) {
			r.Village, r.Area, r.X, r.Y = st.Village, st.Area, st.X, st.Y
			if st.RobotType == 2 || st.RobotType == 3 {
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: true, State: "store", Message: "skip moving store robot"})
				continue
			}
		}
		targetX, targetY := m.randomMoveTarget(r, rc, maps)
		steps := m.randBetween(maxInt(2, rc.MoveSteps-1), minInt(8, rc.MoveSteps+2))
		speeds := make([]int, steps)
		for i := range speeds {
			speeds[i] = m.randBetween(rc.MoveSpeedMin, rc.MoveSpeedMax)
		}
		plans = append(plans, movePlan{robot: r, steps: steps, target: [2]int{targetX, targetY}, speeds: speeds})
		result.Accepted++
		result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted"})
	}
	maxSteps := 0
	for _, plan := range plans {
		maxSteps = maxInt(maxSteps, plan.steps)
	}
	for step := 1; step <= maxSteps; step++ {
		for _, plan := range plans {
			if step > plan.steps {
				continue
			}
			if err := m.dispatchMoveStep(plan.robot, plan.target[0], plan.target[1], step, plan.steps, plan.speeds[step-1], rc); err != nil {
				result.Failed++
			}
		}
		if step < maxSteps {
			delay := rc.MoveStepDelayMS
			if delay > 0 {
				delay = m.randBetween(maxInt(300, delay/2), maxInt(300, delay*3/2))
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	}
	waitMS := rc.MoveStepDelayMS*rc.MoveSteps + 500
	if waitMS < 500 {
		waitMS = 500
	}
	time.Sleep(time.Duration(waitMS) * time.Millisecond)
	status = m.runtimeStatusMap()
	for i := range result.Robots {
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = st.StateName
			result.Confirmed++
		} else if result.Robots[i].State == "accepted" {
			result.Robots[i].State = "pending"
			result.Robots[i].Message = "move not confirmed by runtime state"
			result.Failed++
		}
	}
	return result, nil
}

func (m *RobotManager) Shout(req RobotCommandRequest) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	tpl := m.loadShoutTemplates()
	rc := m.loadRobotConfig()
	result := newCommandResult(len(robots))
	for _, r := range robots {
		worldMsg := safeRobotShoutMessage(tpl.Messages[m.randIntn(len(tpl.Messages))])
		localMsg := safeRobotShoutMessage(tpl.Messages[m.randIntn(len(tpl.Messages))])
		if !rc.ShoutSendEnabled {
			_ = m.appendShout(r.UID, "world", 3, worldMsg)
			_ = m.appendShout(r.UID, "local", 3, localMsg)
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: true, State: "sent", Message: fmt.Sprintf("world=%s local=%s", worldMsg, localMsg)})
		} else {
			if err := m.sendRobotShout("manager", r.UID, tpl, worldMsg, true, rc); err != nil {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
			} else if err := m.sendRobotShout("manager", r.UID, tpl, localMsg, false, rc); err != nil {
				result.Failed++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
			} else {
				result.Accepted++
				result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted", Message: fmt.Sprintf("world=%s local=%s", worldMsg, localMsg)})
			}
		}
		if rc.ShoutDelayMS > 0 {
			delay := m.randBetween(maxInt(200, rc.ShoutDelayMS/2), maxInt(200, rc.ShoutDelayMS*2))
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
	time.Sleep(500 * time.Millisecond)
	status := m.runtimeStatusMap()
	for i := range result.Robots {
		if !strings.EqualFold(result.Robots[i].State, "accepted") {
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = "sent"
			result.Confirmed++
		} else {
			result.Robots[i].State = "not_confirmed"
			if st, ok := status[result.Robots[i].UID]; ok && st.DisconnectReason != 0 {
				result.Robots[i].Message = fmt.Sprintf("%s; disconnect_reason=%d", result.Robots[i].Message, st.DisconnectReason)
			}
			result.Failed++
		}
	}
	return result, nil
}

func (m *RobotManager) ShoutOne(req RobotCommandRequest, world bool) (RobotCommandResult, error) {
	robots, err := m.selectRobots(req)
	if err != nil {
		return RobotCommandResult{}, err
	}
	tpl := m.loadShoutTemplates()
	rc := m.loadRobotConfig()
	channel := "local"
	if world {
		channel = "world"
	}
	result := newCommandResult(len(robots))
	for _, r := range robots {
		msg := safeRobotShoutMessage(tpl.Messages[m.randIntn(len(tpl.Messages))])
		msgType, _, _ := m.prepareShout(tpl, msg, world)
		if !rc.ShoutSendEnabled {
			_ = m.appendShout(r.UID, channel, msgType, msg)
			result.Accepted++
			result.Confirmed++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: true, State: "sent", Message: msg})
		} else if err := m.sendRobotShout("manager", r.UID, tpl, msg, world, rc); err != nil {
			result.Failed++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "failed", Message: err.Error()})
		} else {
			result.Accepted++
			result.Robots = append(result.Robots, RobotActionResult{UID: r.UID, CID: r.CID, OK: false, State: "accepted", Message: msg})
		}
	}
	time.Sleep(500 * time.Millisecond)
	status := m.runtimeStatusMap()
	for i := range result.Robots {
		if !strings.EqualFold(result.Robots[i].State, "accepted") {
			continue
		}
		if st, ok := status[result.Robots[i].UID]; ok && activeRuntimeStatus(st) {
			result.Robots[i].OK = true
			result.Robots[i].State = "sent"
			result.Confirmed++
		} else {
			result.Robots[i].State = "not_confirmed"
			result.Failed++
		}
	}
	return result, nil
}

func (m *RobotManager) autoMoveRobot(r RobotInfo, rc robotRuntimeConfig, maps []mapCatalogItem, follow *followTarget) {
	targetX, targetY := m.randomMoveTarget(r, rc, maps)
	if follow != nil {
		r.Village = follow.Village
		r.Area = follow.Area
		targetX, targetY = m.followMoveTarget(r, *follow, rc, maps)
	}
	steps := m.randBetween(maxInt(2, rc.MoveSteps-1), minInt(8, rc.MoveSteps+2))
	for step := 1; step <= steps; step++ {
		if st, ok := m.runtimeStatusMap()[r.UID]; ok && (st.RobotType == 2 || st.RobotType == 3 || st.StateName != "running") {
			return
		}
		speed := m.randBetween(rc.MoveSpeedMin, rc.MoveSpeedMax)
		_ = m.dispatchMoveStep(r, targetX, targetY, step, steps, speed, rc)
		if step < steps && rc.MoveStepDelayMS > 0 {
			delay := m.randBetween(maxInt(300, rc.MoveStepDelayMS/2), maxInt(300, rc.MoveStepDelayMS*3/2))
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
}

func (m *RobotManager) autoShoutRobot(uid int, tpl shoutTemplates, msg string, world bool) {
	msg = safeRobotShoutMessage(msg)
	rc := m.loadRobotConfig()
	msgType, channel, _ := m.prepareShout(tpl, msg, world)
	if !rc.ShoutSendEnabled {
		_ = m.appendShout(uid, channel, msgType, msg)
		return
	}
	_ = m.sendRobotShout("auto", uid, tpl, msg, world, rc)
}

func (m *RobotManager) sendRobotShout(source string, uid int, tpl shoutTemplates, msg string, world bool, rc robotRuntimeConfig) error {
	msg = safeRobotShoutMessage(msg)
	msgType, channel, outMsg := m.prepareShout(tpl, msg, world)
	body := map[string]interface{}{"userinfos": []map[string]interface{}{{"id": uid, "msg": outMsg, "type": msgType}}}
	data, _ := json.Marshal(body)
	if rc.ShoutDelayMS > 0 {
		time.Sleep(time.Duration(m.randBetween(100, maxInt(100, rc.ShoutDelayMS/2))) * time.Millisecond)
	}
	if _, err := m.doll.MsgPublicMsg(source, string(data)); err != nil {
		return err
	}
	_ = m.appendShoutDetail(uid, channel, msgType, msg, source)
	return nil
}

func (m *RobotManager) prepareShout(_ shoutTemplates, msg string, world bool) (int, string, string) {
	if world {
		return 80, "world", msg + "  服务器喇叭()"
	}
	return 3, "local", msg
}

func (m *RobotManager) appendShout(uid int, channel string, typ int, msg string) error {
	return m.appendShoutDetail(uid, channel, typ, msg, "normal")
}

func (m *RobotManager) appendShoutDetail(uid int, channel string, typ int, msg, source string) error {
	if channel == "" {
		channel = "world"
	}
	_ = msg
	dnf.LogString(dnf.LogLevelIndispensable, fmt.Sprintf("[Shout] source=%s uid=%d channel=%s type=%d\n", source, uid, channel, typ))
	return nil
}

func (m *RobotManager) dispatchMoveStep(r RobotInfo, targetX, targetY, step, steps, speed int, rc robotRuntimeConfig) error {
	startX, startY := r.X, r.Y
	x := startX + (targetX-startX)*step/steps
	y := startY + (targetY-startY)*step/steps
	body := map[string]interface{}{"userinfos": []map[string]interface{}{{
		"id": r.UID, "type": rc.MoveType, "village": r.Village, "area": r.Area,
		"x": x, "y": y, "speed": speed,
	}}}
	data, _ := json.Marshal(body)
	if _, err := m.doll.MsgMove("manager", string(data)); err != nil {
		return err
	}
	if step == steps {
		_, _ = m.db.Exec("UPDATE d_starsky.Dummylist SET curvill=?,curarea=?,curx=?,cury=? WHERE UID=?", r.Village, r.Area, targetX, targetY, r.UID)
	}
	return nil
}

func (m *RobotManager) currentFollowTarget(rc robotRuntimeConfig, maps []mapCatalogItem) (followTarget, bool) {
	account := strings.TrimSpace(rc.FollowAccount)
	if account == "" || rc.SpawnFixed {
		return followTarget{}, false
	}

	rows, err := m.db.Query(`SELECT uid FROM d_starsky.robot_registry WHERE account=? ORDER BY uid DESC LIMIT 20`, account)
	if err == nil {
		var uids []int
		for rows.Next() {
			var uid int
			if rows.Scan(&uid) == nil {
				uids = append(uids, uid)
			}
		}
		_ = rows.Close()
		if len(uids) > 0 {
			status := m.runtimeStatusMap()
			for _, uid := range uids {
				if st, ok := status[uid]; ok && activeRuntimeStatus(st) {
					return followTarget{UID: uid, Village: st.Village, Area: st.Area, X: st.X, Y: st.Y}, true
				}
			}
		}
	}

	var village sql.NullInt64
	err = m.db.QueryRow(`
SELECT COALESCE(NULLIF(s.village,0), c.village)
FROM d_taiwan.accounts a
JOIN taiwan_cain.charac_info c ON c.m_id=a.UID AND c.delete_flag=0
LEFT JOIN taiwan_cain.charac_stat s ON s.charac_no=c.charac_no
WHERE a.accountname=?
ORDER BY c.last_play_time DESC, c.charac_no DESC
LIMIT 1`, account).Scan(&village)
	if err != nil || !village.Valid || village.Int64 <= 0 {
		return followTarget{}, false
	}
	info := RobotInfo{Village: int(village.Int64), Area: rc.SpawnArea, X: m.randBetween(rc.SpawnXMin, rc.SpawnXMax), Y: m.randBetween(rc.SpawnYMin, rc.SpawnYMax), Level: rc.LevelMax}
	m.applyVillageLocation(&info, info.Village, rc, maps)
	return followTarget{Village: info.Village, Area: info.Area, X: info.X, Y: info.Y}, true
}

func (m *RobotManager) followMoveTarget(r RobotInfo, target followTarget, rc robotRuntimeConfig, maps []mapCatalogItem) (int, int) {
	xMin, xMax := target.X-rc.FollowRadiusX, target.X+rc.FollowRadiusX
	yMin, yMax := target.Y-rc.FollowRadiusY, target.Y+rc.FollowRadiusY
	if rc.FollowRadiusX <= 0 {
		xMin, xMax = target.X-120, target.X+120
	}
	if rc.FollowRadiusY <= 0 {
		yMin, yMax = target.Y-30, target.Y+30
	}
	for _, mp := range maps {
		if mp.Use && mp.Village == target.Village && mp.Area == target.Area {
			xMin = maxInt(mp.XMin, xMin)
			xMax = minInt(mp.XMax, xMax)
			yMin = maxInt(mp.YMin, yMin)
			yMax = minInt(mp.YMax, yMax)
			break
		}
	}
	if xMax <= xMin || yMax <= yMin {
		return m.randomMoveTarget(r, rc, maps)
	}
	return m.randBetween(xMin, xMax), m.randBetween(yMin, yMax)
}

func (m *RobotManager) randomMoveTarget(r RobotInfo, rc robotRuntimeConfig, maps []mapCatalogItem) (int, int) {
	xMin, xMax := rc.SpawnXMin, rc.SpawnXMax
	yMin, yMax := rc.SpawnYMin, rc.SpawnYMax
	if !rc.SpawnFixed {
		for _, mp := range maps {
			if mp.Village == r.Village && mp.Area == r.Area {
				xMin, xMax = mp.XMin, mp.XMax
				yMin, yMax = mp.YMin, mp.YMax
				break
			}
		}
	}
	if xMax <= xMin {
		xMin, xMax = maxInt(0, r.X-120), r.X+120
	}
	if yMax <= yMin {
		yMin, yMax = maxInt(0, r.Y-40), r.Y+40
	}
	for i := 0; i < 12; i++ {
		x := m.randBetween(xMin, xMax)
		y := m.randBetween(yMin, yMax)
		if absInt(x-r.X) >= 40 || absInt(y-r.Y) >= 15 {
			return x, y
		}
	}
	return m.randBetween(xMin, xMax), m.randBetween(yMin, yMax)
}
