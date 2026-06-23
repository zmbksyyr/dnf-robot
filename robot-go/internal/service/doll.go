package service

import "encoding/json"

type Result struct {
	Msg string
}

func NewResult(msg string) Result {
	return Result{Msg: msg}
}

type RobotService interface {
	PushRobotMsg(msgFlag byte, fd int, msgType int, jsonData json.RawMessage) string
	CallRobotMsgResult(msgFlag byte, fd int, msgType int, jsonData json.RawMessage) (string, error)
}

var robotSvc RobotService

func SetRobotService(svc RobotService) {
	robotSvc = svc
}

func listDoll(key string) Result {
	var v interface{}
	if err := json.Unmarshal([]byte(key), &v); err != nil {
		return NewResult("invalid json")
	}
	if v == nil {
		return NewResult("invalid json")
	}
	if _, ok := v.(map[string]interface{}); !ok {
		return NewResult("invalid json")
	}
	resultMsg, _ := robotSvc.CallRobotMsgResult(0, 1, 6001, []byte(key))
	return NewResult(resultMsg)
}

func msgRemove(keyData string) Result {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(keyData), &v); err != nil {
		return NewResult("invalid json")
	}
	if v == nil {
		return NewResult("invalid json")
	}
	if _, ok := v["userinfos"]; !ok {
		return NewResult("missing userinfos")
	}
	infos, ok := v["userinfos"].([]interface{})
	if !ok {
		return NewResult("invalid userinfos")
	}
	_ = infos
	robotSvc.PushRobotMsg(0, 1, 6003, []byte(keyData))
	return NewResult("ok")
}

func msgLogout(keyData string) Result {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(keyData), &v); err != nil {
		return NewResult("invalid json")
	}
	if v == nil {
		return NewResult("invalid json")
	}
	if _, ok := v["userinfos"]; !ok {
		return NewResult("missing userinfos")
	}
	infos, ok := v["userinfos"].([]interface{})
	if !ok {
		return NewResult("invalid userinfos")
	}
	_ = infos
	robotSvc.PushRobotMsg(0, 1, 6005, []byte(keyData))
	return NewResult("ok")
}

func msgMove(keyData string) Result {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(keyData), &v); err != nil {
		return NewResult("invalid json")
	}
	if v == nil {
		return NewResult("invalid json")
	}
	if _, ok := v["userinfos"]; !ok {
		return NewResult("missing userinfos")
	}
	infos, ok := v["userinfos"].([]interface{})
	if !ok {
		return NewResult("invalid userinfos")
	}
	_ = infos
	robotSvc.PushRobotMsg(0, 1, 6006, []byte(keyData))
	return NewResult("ok")
}

func msgPublicMsg(keyData string) Result {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(keyData), &v); err != nil {
		return NewResult("invalid json")
	}
	if v == nil {
		return NewResult("invalid json")
	}
	if _, ok := v["userinfos"]; !ok {
		return NewResult("missing userinfos")
	}
	infos, ok := v["userinfos"].([]interface{})
	if !ok {
		return NewResult("invalid userinfos")
	}
	_ = infos
	robotSvc.PushRobotMsg(0, 1, 6007, []byte(keyData))
	return NewResult("ok")
}

func msgOnLine(keyData string) Result {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(keyData), &v); err != nil {
		return NewResult("invalid json")
	}
	if v == nil {
		return NewResult("invalid json")
	}
	if _, ok := v["userinfos"]; !ok {
		return NewResult("missing userinfos")
	}
	infos, ok := v["userinfos"].([]interface{})
	if !ok {
		return NewResult("invalid userinfos")
	}

	for i, ui := range infos {
		userinfo, ok := ui.(map[string]interface{})
		if !ok {
			continue
		}
		uidVal, ok := userinfo["uid"]
		if !ok {
			continue
		}
		uid, ok := toInt(uidVal)
		if !ok || uid <= 0 {
			continue
		}
		loginkey := GetLoginKey(uid)
		if len(loginkey) > 0 {
			userinfo["token"] = loginkey
			infos[i] = userinfo
		}
	}
	v["userinfos"] = infos
	data, _ := json.Marshal(v)
	robotSvc.PushRobotMsg(0, 1, 6008, data)
	return NewResult("ok")
}

func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}

type DollService struct{}

func NewDollService() *DollService {
	return &DollService{}
}

func (d *DollService) ListDoll(clientID string, keyData string) (string, error) {
	result := listDoll(keyData)
	return result.Msg, nil
}

func (d *DollService) MsgRemove(clientID string, keyData string) (string, error) {
	result := msgRemove(keyData)
	return result.Msg, nil
}

func (d *DollService) MsgLogout(clientID string, keyData string) (string, error) {
	result := msgLogout(keyData)
	return result.Msg, nil
}

func (d *DollService) MsgMove(clientID string, keyData string) (string, error) {
	result := msgMove(keyData)
	return result.Msg, nil
}

func (d *DollService) MsgPublicMsg(clientID string, keyData string) (string, error) {
	result := msgPublicMsg(keyData)
	return result.Msg, nil
}

func (d *DollService) MsgOnLine(clientID string, keyData string) (string, error) {
	result := msgOnLine(keyData)
	return result.Msg, nil
}

func (d *DollService) RuntimeStatus() []RuntimeRobotStatus {
	if svc, ok := robotSvc.(*RobotSvc); ok {
		return svc.RuntimeStatus()
	}
	return nil
}

func (d *DollService) StartPrivateStore(uid int, title string) bool {
	if svc, ok := robotSvc.(*RobotSvc); ok {
		return svc.StartPrivateStore(uid, title)
	}
	return false
}

func (d *DollService) ResetPrivateStore(uid int) bool {
	if svc, ok := robotSvc.(*RobotSvc); ok {
		return svc.ResetPrivateStore(uid)
	}
	return false
}

func (d *DollService) SetArea(uid int, village, area int, x, y int) bool {
	if svc, ok := robotSvc.(*RobotSvc); ok {
		return svc.SetArea(uid, village, area, x, y)
	}
	return false
}

func (d *DollService) SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool {
	if svc, ok := robotSvc.(*RobotSvc); ok {
		return svc.SetAreaFrom(uid, village, area, x, y, fromVillage, fromArea)
	}
	return false
}

func (d *DollService) CompletePrivateStoreDisplay(uid int) bool {
	if svc, ok := robotSvc.(*RobotSvc); ok {
		return svc.CompletePrivateStoreDisplay(uid)
	}
	return false
}
