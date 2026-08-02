package dnfruntime

import (
	"time"

	"robot/internal/protocol/dnf"
	"robot/internal/shared"
)

func (rs *RobotSvc) StartPrivateStore(uid int, title string) bool {
	if !rs.beginWork() {
		return false
	}
	vo := rs.robot(uid)
	if vo == nil {
		rs.worker.Done()
		return false
	}
	snap := vo.Snapshot()
	if shared.StateName(int(snap.State)) != shared.RuntimeStateRunning || snap.PartyActive {
		rs.worker.Done()
		return false
	}
	vo.PreparePrivateStoreState(title)
	go func() {
		defer rs.worker.Done()
		completePrivateStore(rs.stop, uid, vo)
	}()
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

func completePrivateStore(stop <-chan struct{}, uid int, vo *dnf.RobotVo) {
	defer func() {
		if r := recover(); r != nil {
			robotLogf("[StartPrivateStore] panic uid=%d err=%v\n", uid, r)
		}
	}()
	if !waitStoreDelay(stop, time.Duration(uid%7)*450*time.Millisecond) {
		return
	}
	if !storeRobotReady(vo) {
		return
	}
	// Request the authoritative inventory while the character is still in the
	// normal town state. Some DFGamer builds stop returning the full CMD 13 body
	// once private-store creation has advanced, so retain this pre-create
	// snapshot and request it again immediately after CMD 88 below.
	if !vo.GetCompleteDisplay(0) {
		return
	}
	if !waitStoreItemList(stop, vo, 1500*time.Millisecond) {
		return
	}
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
	if !waitStoreCreated(stop, vo, 5*time.Second) {
		return
	}
	if snap := vo.Snapshot(); snap.PartyActive || shared.StateName(int(snap.State)) != shared.RuntimeStateRunning {
		return
	} else if !snap.StoreCreated {
		vo.MarkPrivateStoreCreateFailed()
		return
	}
	if !waitStoreItemList(stop, vo, 2*time.Second) {
		return
	}
	// The offline preparation transaction already knows the exact global slots
	// written into the inventory image. After NoCache and the confirmed relogin,
	// let CMD 90 validate that image directly instead of waiting on a randomly
	// encrypted DPROTO inventory response.
	if vo.GetDbDataAndCompleteDisplay() {
		return
	}
	if !vo.PrivateStoreItemListReceived() {
		_ = vo.GetCompleteDisplay(0)
		// This DFGamer can return one or more empty CMD 13 placeholders before
		// publishing the complete inventory. Give the second request enough time
		// to win the seven-item path. If no complete reply arrives, the workflow
		// refreshes the session once instead of guessing database slot indexes.
		if !waitStoreItemList(stop, vo, 10*time.Second) {
			return
		}
	}
	if !storeRobotReady(vo) {
		return
	}
	// CMD 90 remains the authoritative validation boundary for either snapshot.
	_ = vo.GetDbDataAndCompleteDisplay()
	vo.MarkPrivateStoreDisplayFailed()
}

func storeRobotReady(vo *dnf.RobotVo) bool {
	snap := vo.Snapshot()
	return !snap.PartyActive && shared.StateName(int(snap.State)) == shared.RuntimeStateRunning
}

func waitStoreCreated(stop <-chan struct{}, vo *dnf.RobotVo, timeout time.Duration) bool {
	return waitStoreCondition(stop, timeout, func() bool {
		snap := vo.Snapshot()
		return snap.PartyActive || snap.StoreCreated || snap.StoreCreateRejected || shared.StateName(int(snap.State)) != shared.RuntimeStateRunning
	})
}

func waitStoreItemList(stop <-chan struct{}, vo *dnf.RobotVo, timeout time.Duration) bool {
	return waitStoreCondition(stop, timeout, func() bool {
		return vo.PrivateStoreItemListReceived() || !storeRobotReady(vo)
	})
}

func waitStoreCondition(stop <-chan struct{}, timeout time.Duration, done func() bool) bool {
	if done() {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return false
		case <-timer.C:
			return true
		case <-ticker.C:
			if done() {
				return true
			}
		}
	}
}

func waitStoreDelay(stop <-chan struct{}, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-stop:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-timer.C:
		return true
	}
}
