package dnfruntime

import (
	"encoding/json"
	"robot/internal/foundation/lockhub"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/protocol/dnf"
	"robot/internal/shared"
	"sync"
	"time"
)

// ---- service.go ----
type RobotSvc struct {
	mu       lockhub.Locker
	msgQueue []robotMsgEntry
	cond     *sync.Cond
	running  bool
	table    *dnf.DnfTableDrive
}

type robotMsgEntry struct {
	MsgFlag byte
	FD      int
	MsgType int
	JSON    json.RawMessage
}

func robotLogf(format string, args ...interface{}) {
	foundationlog.Robotf(format, args...)
}

var robotServiceInstance *RobotSvc
var robotServiceOnce sync.Once

func GetRobotService() *RobotSvc {
	robotServiceOnce.Do(func() {
		robotServiceInstance = &RobotSvc{}
		robotServiceInstance.cond = sync.NewCond(&robotServiceInstance.mu)
	})
	return robotServiceInstance
}

func (rs *RobotSvc) Init() {
	rs.table = dnf.NewDnfTableDrive()
	rs.running = true
	go rs.run()
}

func (rs *RobotSvc) PushRobotMsg(msgFlag byte, fd int, msgType int, jsonData json.RawMessage) string {
	entry := robotMsgEntry{
		MsgFlag: msgFlag,
		FD:      fd,
		MsgType: msgType,
		JSON:    jsonData,
	}
	rs.mu.Lock()
	if len(rs.msgQueue) >= 10000 {
		rs.mu.Unlock()
		robotLogf("[RobotSvc] msg_queue_full type=%d len=%d\n", msgType, 10000)
		return "queue_full"
	}
	rs.msgQueue = append(rs.msgQueue, entry)
	rs.mu.Unlock()
	rs.cond.Signal()
	return ""
}

func (rs *RobotSvc) CallRobotMsgResult(msgFlag byte, fd int, msgType int, jsonData json.RawMessage) (string, error) {
	if rs.table == nil {
		return "", nil
	}
	msg := dnf.RobotMsg{
		MsgFlag: msgFlag,
		Fd:      fd,
		MsgType: dnf.MsgType(msgType),
		JSON:    jsonData,
	}
	result := rs.table.HandleKeyword(msg)
	return result.Msg, nil
}

func (rs *RobotSvc) RuntimeStatus() []shared.RuntimeStatus {
	if rs.table == nil {
		return nil
	}
	robotMap := rs.table.GetTask().GetRobotVoMap()
	out := make([]shared.RuntimeStatus, 0, len(robotMap))
	now := uint32(time.Now().Unix())
	for _, vo := range robotMap {
		snap := vo.Snapshot()
		state := int(snap.State)
		out = append(out, shared.RuntimeStatus{
			UID:                  int(snap.UID),
			CID:                  int(snap.CID),
			State:                state,
			StateName:            robotStateName(state),
			LastError:            int(snap.LastError),
			DisconnectReason:     int(snap.DisconnectReason),
			Reconnects:           int(snap.Reconnects),
			RunStartTime:         int64(snap.RunStartTime),
			UptimeSeconds:        uptimeSeconds(now, snap.RunStartTime),
			RobotType:            snap.RobotType,
			StoreDisplaySent:     snap.StoreDisplaySent,
			StoreDisplayAck:      snap.StoreDisplayAck,
			StoreDisplayRejected: snap.StoreDisplayRejected,
			StoreCreateRejected:  snap.StoreCreateRejected,
			LastStoreError:       snap.LastStoreError,
			StoreCreated:         snap.StoreCreated,
			Village:              int(snap.Village),
			Area:                 int(snap.Area),
			X:                    int(snap.X),
			Y:                    int(snap.Y),
		})
	}
	return out
}

func uptimeSeconds(now, start uint32) int {
	if start == 0 {
		return 0
	}
	if now < start {
		return 0
	}
	return int(now - start)
}

func (rs *RobotSvc) StartPrivateStore(uid int, title string) bool {
	if rs.table == nil || uid <= 0 {
		return false
	}
	vo := rs.table.GetTask().Find(uid)
	if vo == nil {
		return false
	}
	snap := vo.Snapshot()
	if robotStateName(int(snap.State)) != "running" {
		return false
	}
	vo.PreparePrivateStoreState(title)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				robotLogf("[StartPrivateStore] PANIC uid=%d err=%v\n", uid, r)
			}
		}()
		time.Sleep(time.Duration(uid%7) * 450 * time.Millisecond)
		vo.CreatePrivateStore()
		waitStoreCreated(vo, 5*time.Second)
		if snap := vo.Snapshot(); !snap.StoreCreated && robotStateName(int(snap.State)) == "running" {
			time.Sleep(1200 * time.Millisecond)
			vo.CreatePrivateStore()
			waitStoreCreated(vo, 5*time.Second)
		}
		time.Sleep(1200 * time.Millisecond)
		if snap := vo.Snapshot(); robotStateName(int(snap.State)) != "running" {
			return
		}
		vo.GetCompleteDisplay(0)
		time.Sleep(1200 * time.Millisecond)
		vo.GetDbDataAndCompleteDisplay()
		waitStoreDisplay(vo, 1500*time.Millisecond)
		if snap := vo.Snapshot(); snap.StoreDisplayAck || robotStateName(int(snap.State)) != "running" {
			return
		}
		vo.CompleteDisplayFromStallFallback()
	}()
	return true
}

func (rs *RobotSvc) ResetPrivateStore(uid int) bool {
	if rs.table == nil || uid <= 0 {
		return false
	}
	vo := rs.table.GetTask().Find(uid)
	if vo == nil {
		return false
	}
	vo.ResetPrivateStoreState()
	return true
}

func (rs *RobotSvc) SetArea(uid int, village, area int, x, y int) bool {
	if rs.table == nil || uid <= 0 {
		return false
	}
	vo := rs.table.GetTask().Find(uid)
	if vo == nil {
		return false
	}
	if snap := vo.Snapshot(); robotStateName(int(snap.State)) != "running" {
		return false
	}
	vo.SetArea(uint8(village), uint8(area), uint16(x), uint16(y))
	return true
}

func (rs *RobotSvc) SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool {
	if rs.table == nil || uid <= 0 {
		return false
	}
	vo := rs.table.GetTask().Find(uid)
	if vo == nil {
		return false
	}
	if snap := vo.Snapshot(); robotStateName(int(snap.State)) != "running" {
		return false
	}
	vo.SetAreaFrom(uint8(village), uint8(area), uint16(x), uint16(y), uint16(fromVillage), uint16(fromArea))
	return true
}

func (rs *RobotSvc) CompletePrivateStoreDisplay(uid int) bool {
	if rs.table == nil || uid <= 0 {
		return false
	}
	vo := rs.table.GetTask().Find(uid)
	if vo == nil {
		return false
	}
	return vo.CompleteDisplayFromStallFallback()
}

func waitStoreCreated(vo *dnf.RobotVo, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap := vo.Snapshot()
		if snap.StoreCreated || snap.StoreCreateRejected || robotStateName(int(snap.State)) != "running" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitStoreDisplay(vo *dnf.RobotVo, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap := vo.Snapshot()
		if snap.StoreDisplayAck || robotStateName(int(snap.State)) != "running" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func robotStateName(state int) string {
	return shared.StateName(state)
}

func (rs *RobotSvc) run() {
	for rs.running {
		rs.mu.Lock()
		for len(rs.msgQueue) == 0 && rs.running {
			rs.cond.Wait()
		}
		if !rs.running {
			rs.mu.Unlock()
			return
		}
		var batch []robotMsgEntry
		for len(rs.msgQueue) > 0 {
			batch = append(batch, rs.msgQueue[0])
			rs.msgQueue = rs.msgQueue[1:]
		}
		rs.mu.Unlock()

		for _, entry := range batch {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						robotLogf("[RobotSvc] dispatch panic type=%d err=%v\n", entry.MsgType, rec)
					}
				}()
				msg := dnf.RobotMsg{
					MsgFlag: entry.MsgFlag,
					Fd:      entry.FD,
					MsgType: dnf.MsgType(entry.MsgType),
					JSON:    entry.JSON,
				}
				rs.table.HandleKeyword(msg)
			}()
		}
	}
}

func (rs *RobotSvc) Shutdown() {
	rs.mu.Lock()
	rs.running = false
	rs.cond.Broadcast()
	rs.mu.Unlock()
	if rs.table != nil {
		rs.table.Shutdown()
	}
}

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
var loginKeyProvider func(uid int) string

func SetRobotService(svc RobotService) {
	robotSvc = svc
}

func SetLoginKeyProvider(provider func(uid int) string) {
	loginKeyProvider = provider
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
		var loginkey string
		if loginKeyProvider != nil {
			loginkey = loginKeyProvider(uid)
		}
		if len(loginkey) > 0 {
			userinfo["token"] = loginkey
		}
		infos[i] = userinfo
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

func (d *DollService) RuntimeStatus() []shared.RuntimeStatus {
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
