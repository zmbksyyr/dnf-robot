package dnfruntime

import (
	"errors"
	"sync"
	"time"

	"robot/internal/foundation/lockhub"
	foundationlog "robot/internal/foundation/log"
	"robot/internal/protocol/dnf"
	"robot/internal/shared"
)

const maxRobotCommandQueue = 10000

var (
	ErrRuntimeStopped   = errors.New("robot runtime is stopped")
	ErrCommandQueueFull = errors.New("robot runtime command queue is full")
)

type RobotSvc struct {
	mu           lockhub.Locker
	msgQueue     []robotMsgEntry
	cond         *sync.Cond
	running      bool
	table        robotDriver
	worker       sync.WaitGroup
	shutdownOnce sync.Once
}

type robotDriver interface {
	DispatchLogout(uid int) dnf.DnfTableTaskResult
	DispatchMove(command shared.RuntimeMoveCommand) dnf.DnfTableTaskResult
	DispatchOnline(users []shared.RuntimeOnlineUser) dnf.DnfTableTaskResult
	DispatchShout(command shared.RuntimeShoutCommand) dnf.DnfTableTaskResult
	GetTask() *dnf.RobotDnfTask
	Shutdown()
}

type robotCommandType uint8

const (
	robotCommandLogout robotCommandType = iota + 1
	robotCommandMove
	robotCommandOnline
	robotCommandShout
)

type robotMsgEntry struct {
	typ    robotCommandType
	uid    int
	move   shared.RuntimeMoveCommand
	online []shared.RuntimeOnlineUser
	shout  shared.RuntimeShoutCommand
}

func NewRobotService() *RobotSvc {
	return newRobotService(dnf.NewDnfTableDrive())
}

func newRobotService(table robotDriver) *RobotSvc {
	rs := &RobotSvc{table: table, running: true}
	rs.cond = sync.NewCond(&rs.mu)
	rs.worker.Add(1)
	go rs.run()
	return rs
}

func (rs *RobotSvc) Logout(uid int) error {
	return rs.enqueue(robotMsgEntry{typ: robotCommandLogout, uid: uid})
}

func (rs *RobotSvc) ForceClose(uid int) bool {
	task := rs.task()
	if task == nil || uid <= 0 {
		return false
	}
	vo := task.Find(uid)
	if vo == nil {
		return true
	}
	if !vo.TryCloseOut() {
		robotLogf("[RobotSvc] force_close_busy uid=%d\n", uid)
		return false
	}
	return task.DeleteIf(uint32(uid), vo)
}

func (rs *RobotSvc) InvalidateLoginRepairs(uids []int) {
	dnf.InvalidateLoginRepairs(uids)
}

func (rs *RobotSvc) Move(command shared.RuntimeMoveCommand) error {
	return rs.enqueue(robotMsgEntry{typ: robotCommandMove, move: command})
}

func (rs *RobotSvc) Online(users []shared.RuntimeOnlineUser) error {
	commandUsers := append([]shared.RuntimeOnlineUser(nil), users...)
	return rs.enqueue(robotMsgEntry{typ: robotCommandOnline, online: commandUsers})
}

func (rs *RobotSvc) Shout(command shared.RuntimeShoutCommand) error {
	return rs.enqueue(robotMsgEntry{typ: robotCommandShout, shout: command})
}

func (rs *RobotSvc) enqueue(entry robotMsgEntry) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.running || rs.table == nil {
		return ErrRuntimeStopped
	}
	if len(rs.msgQueue) >= maxRobotCommandQueue {
		robotLogf("[RobotSvc] command_queue_full type=%s len=%d\n", entry.typ, maxRobotCommandQueue)
		return ErrCommandQueueFull
	}
	rs.msgQueue = append(rs.msgQueue, entry)
	rs.cond.Signal()
	return nil
}

func (rs *RobotSvc) run() {
	defer rs.worker.Done()
	for {
		rs.mu.Lock()
		for len(rs.msgQueue) == 0 && rs.running {
			rs.cond.Wait()
		}
		if !rs.running && len(rs.msgQueue) == 0 {
			rs.mu.Unlock()
			return
		}
		batch := rs.msgQueue
		rs.msgQueue = nil
		rs.mu.Unlock()

		for _, entry := range batch {
			rs.dispatch(entry)
		}
	}
}

func (rs *RobotSvc) dispatch(entry robotMsgEntry) {
	defer func() {
		if rec := recover(); rec != nil {
			robotLogf("[RobotSvc] dispatch_panic type=%s err=%v\n", entry.typ, rec)
		}
	}()

	var result dnf.DnfTableTaskResult
	switch entry.typ {
	case robotCommandLogout:
		result = rs.table.DispatchLogout(entry.uid)
	case robotCommandMove:
		result = rs.table.DispatchMove(entry.move)
	case robotCommandOnline:
		result = rs.table.DispatchOnline(entry.online)
	case robotCommandShout:
		result = rs.table.DispatchShout(entry.shout)
	default:
		robotLogf("[RobotSvc] unknown_command type=%d\n", entry.typ)
		return
	}
	if result.Code != 200 {
		robotLogf("[RobotSvc] command_rejected type=%s msg=%s\n", entry.typ, result.Msg)
	}
}

func (rs *RobotSvc) Shutdown() {
	if rs == nil {
		return
	}
	rs.shutdownOnce.Do(func() {
		rs.mu.Lock()
		rs.running = false
		rs.cond.Broadcast()
		rs.mu.Unlock()
		rs.worker.Wait()
		if rs.table != nil {
			rs.table.Shutdown()
		}
	})
}

func (rs *RobotSvc) RuntimeStatus() []shared.RuntimeStatus {
	task := rs.task()
	if task == nil {
		return nil
	}
	robotMap := task.GetRobotVoMap()
	out := make([]shared.RuntimeStatus, 0, len(robotMap))
	now := uint32(time.Now().Unix())
	for _, vo := range robotMap {
		snap, _ := vo.TrySnapshot()
		if snap.UID == 0 {
			continue
		}
		state := int(snap.State)
		out = append(out, shared.RuntimeStatus{
			UID:                  int(snap.UID),
			CID:                  int(snap.CID),
			State:                state,
			StateName:            shared.StateName(state),
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
			DisjointCreateSent:   snap.DisjointCreateSent,
			DisjointDirectAck:    snap.DisjointDirectAck,
			DisjointActive:       snap.DisjointActive,
			LastDisjointError:    snap.LastDisjointError,
			PartyActive:          snap.PartyActive,
			Village:              int(snap.Village),
			Area:                 int(snap.Area),
			X:                    int(snap.X),
			Y:                    int(snap.Y),
		})
	}
	return out
}

func (rs *RobotSvc) PartyActive(uid int) bool {
	task := rs.task()
	if task == nil || uid <= 0 {
		return false
	}
	vo := task.Find(uid)
	return vo != nil && vo.Snapshot().PartyActive
}

func (rs *RobotSvc) task() *dnf.RobotDnfTask {
	if rs == nil || rs.table == nil {
		return nil
	}
	return rs.table.GetTask()
}

func (typ robotCommandType) String() string {
	switch typ {
	case robotCommandLogout:
		return "logout"
	case robotCommandMove:
		return "move"
	case robotCommandOnline:
		return "online"
	case robotCommandShout:
		return "shout"
	default:
		return "unknown"
	}
}

func uptimeSeconds(now, start uint32) int {
	if start == 0 || now < start {
		return 0
	}
	return int(now - start)
}

func robotLogf(format string, args ...interface{}) {
	foundationlog.Robotf(format, args...)
}
