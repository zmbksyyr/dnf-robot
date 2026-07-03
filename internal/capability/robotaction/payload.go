package robotaction

import (
	"encoding/json"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

func LogoutPayload(uid int) string {
	body := map[string]interface{}{"userinfos": []map[string]interface{}{{"id": uid}}}
	data, _ := json.Marshal(body)
	return string(data)
}

func OnlinePayload(userinfos []map[string]interface{}) string {
	body := map[string]interface{}{"userinfos": userinfos}
	data, _ := json.Marshal(body)
	return string(data)
}

func MovePayload(info robotcap.Info, targetX, targetY, step, steps, speed int, rc robotconfig.RuntimeConfig) string {
	startX, startY := info.X, info.Y
	x := startX + (targetX-startX)*step/steps
	y := startY + (targetY-startY)*step/steps
	body := map[string]interface{}{"userinfos": []map[string]interface{}{{
		"id": info.UID, "type": rc.MoveType, "village": info.Village, "area": info.Area,
		"x": x, "y": y, "speed": speed,
	}}}
	data, _ := json.Marshal(body)
	return string(data)
}

func LocalShoutPayload(uid int, msg string, msgType int) string {
	body := map[string]interface{}{"userinfos": []map[string]interface{}{{"id": uid, "msg": msg, "type": msgType}}}
	data, _ := json.Marshal(body)
	return string(data)
}
