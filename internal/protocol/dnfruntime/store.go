package dnfruntime

import (
	"time"

	"robot/internal/protocol/dnf"
	"robot/internal/shared"
)

func (rs *RobotSvc) StartPrivateStore(uid int, title string) bool {
	vo := rs.robot(uid)
	if vo == nil {
		return false
	}
	snap := vo.Snapshot()
	if shared.StateName(int(snap.State)) != shared.RuntimeStateRunning || snap.PartyActive {
		return false
	}
	vo.PreparePrivateStoreState(title)
	go completePrivateStore(uid, vo)
	return true
}

func (rs *RobotSvc) StartDisjointStore(uid int, cost uint32) bool {
	vo := rs.runningRobot(uid)
	return vo != nil && vo.OpenDisjointStore(cost)
}

func (rs *RobotSvc) ResetPrivateStore(uid int) bool {
	vo := rs.robot(uid)
	if vo == nil {
		return false
	}
	vo.ResetPrivateStoreState()
	return true
}

func (rs *RobotSvc) ResetDisjointStore(uid int) bool {
	vo := rs.robot(uid)
	if vo == nil {
		return false
	}
	vo.ResetDisjointStoreState()
	return true
}

func (rs *RobotSvc) SetArea(uid int, village, area int, x, y int) bool {
	vo := rs.runningRobot(uid)
	if vo == nil {
		return false
	}
	vo.SetArea(uint8(village), uint8(area), uint16(x), uint16(y))
	return true
}

func (rs *RobotSvc) SetAreaFrom(uid int, village, area int, x, y int, fromVillage, fromArea int) bool {
	vo := rs.runningRobot(uid)
	if vo == nil {
		return false
	}
	vo.SetAreaFrom(uint8(village), uint8(area), uint16(x), uint16(y), uint16(fromVillage), uint16(fromArea))
	return true
}

func (rs *RobotSvc) robot(uid int) *dnf.RobotVo {
	task := rs.task()
	if task == nil || uid <= 0 {
		return nil
	}
	return task.Find(uid)
}

func (rs *RobotSvc) runningRobot(uid int) *dnf.RobotVo {
	vo := rs.robot(uid)
	if vo == nil {
		return nil
	}
	snap := vo.Snapshot()
	if shared.StateName(int(snap.State)) != shared.RuntimeStateRunning || snap.PartyActive {
		return nil
	}
	return vo
}

func completePrivateStore(uid int, vo *dnf.RobotVo) {
	defer func() {
		if r := recover(); r != nil {
			robotLogf("[StartPrivateStore] panic uid=%d err=%v\n", uid, r)
		}
	}()
	time.Sleep(time.Duration(uid%7) * 450 * time.Millisecond)
	if !storeRobotReady(vo) {
		return
	}
	if !vo.CreatePrivateStore() {
		return
	}
	// DFGamer variants can require CMD 20 immediately after CMD 88 and may stop
	// replying once the store reaches state 1. Do not serialize the inventory
	// request behind the create acknowledgement.
	if !vo.GetCompleteDisplay(0) {
		return
	}
	waitStoreCreated(vo, 5*time.Second)
	if snap := vo.Snapshot(); snap.PartyActive || shared.StateName(int(snap.State)) != shared.RuntimeStateRunning {
		return
	} else if !snap.StoreCreated {
		vo.MarkPrivateStoreCreateFailed()
		return
	}
	waitStoreItemList(vo, 2*time.Second)
	if !vo.PrivateStoreItemListReceived() {
		_ = vo.GetCompleteDisplay(0)
		waitStoreItemList(vo, 4*time.Second)
	}
	if !storeRobotReady(vo) {
		return
	}
	_ = vo.GetDbDataAndCompleteDisplay()
	if snap := vo.Snapshot(); !snap.StoreDisplaySent && !snap.StoreDisplayAck && !snap.StoreDisplayRejected {
		vo.CompleteDisplayFromStallFallback()
	}
	vo.MarkPrivateStoreDisplayFailed()
}

func storeRobotReady(vo *dnf.RobotVo) bool {
	snap := vo.Snapshot()
	return !snap.PartyActive && shared.StateName(int(snap.State)) == shared.RuntimeStateRunning
}

func waitStoreCreated(vo *dnf.RobotVo, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap := vo.Snapshot()
		if snap.PartyActive || snap.StoreCreated || snap.StoreCreateRejected || shared.StateName(int(snap.State)) != shared.RuntimeStateRunning {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitStoreItemList(vo *dnf.RobotVo, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if vo.PrivateStoreItemListReceived() || !storeRobotReady(vo) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
