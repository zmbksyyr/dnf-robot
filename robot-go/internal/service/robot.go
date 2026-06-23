package service

import (
	"encoding/json"
	"sync"
	"time"

	"robot/internal/dnf"
)

type RobotSvc struct {
	mu       sync.Mutex
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

type RuntimeRobotStatus struct {
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
	StoreCreated         bool
	Village              int
	Area                 int
	X                    int
	Y                    int
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

func (rs *RobotSvc) RuntimeStatus() []RuntimeRobotStatus {
	if rs.table == nil {
		return nil
	}
	robotMap := rs.table.GetTask().GetRobotVoMap()
	out := make([]RuntimeRobotStatus, 0, len(robotMap))
	now := uint32(time.Now().Unix())
	for _, vo := range robotMap {
		snap := vo.Snapshot()
		state := int(snap.State)
		out = append(out, RuntimeRobotStatus{
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
		} else if !snap.StoreCreated {
			robotLogf("[StartPrivateStore] uid=%d created_ack_missing_try_display\n", uid)
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
	switch state {
	case 0:
		return "stop"
	case 1:
		return "init"
	case 2:
		return "login"
	case 3:
		return "running"
	case 4:
		return "clean"
	case 5:
		return "wrong"
	default:
		return "unknown"
	}
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
