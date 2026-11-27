package dnf

import (
	"time"

	foundationlog "robot/internal/foundation/log"
)

func (r *RobotVo) rejectInventoryLoginUnsafe() {
	r.closeOutUnsafe()
	// Keep the small terminal snapshot in the registry until explicit logout
	// or a new login request. The scheduler must see the original rejection.
	r.State = StateWrong
	r.publishSnapshotUnsafe()
	foundationlog.Robotf("[RobotVo] login_rejected uid=%d cid=%d disconnect_reason=%d action=check_inventory_before_retry\n", r.UID, r.CID, r.DisconReason)
}

func (r *RobotVo) CheckUserState() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop || r.State == StateWrong {
		return false
	}
	if r.DisconReason == InventoryRejected {
		r.rejectInventoryLoginUnsafe()
		return false
	}
	if r.DisconReason != NoDisconnect {
		r.logPartyTransportClearedUnsafe("disconnect reason")
		r.stopPartySupervisorUnsafe()
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		r.publishSnapshotUnsafe()
		return false
	}
	if r.State != StateRun {
		r.logPartyTransportClearedUnsafe("runtime stopped")
		r.stopPartySupervisorUnsafe()
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		r.publishSnapshotUnsafe()
		return false
	}
	partyActive := r.partyActiveUnsafe()
	if partyActive {
		r.ensurePartySupervisorUnsafe()
		r.ensurePartyUDPLoopUnsafe()
		r.ensurePartyRelayUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()
	} else {
		r.stopPartySupervisorUnsafe()
		if r.partyRelayConn != nil {
			r.closePartyRelayUnsafe()
		}
	}
	return true
}

func (r *RobotVo) RefishConnect() bool {
	r.mu.Lock()
	controller := r.Controller
	uid := r.UID

	if r.State == StateStop {
		r.logPartyTransportClearedUnsafe("refresh stopped session")
		r.stopPartySupervisorUnsafe()
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.mu.Unlock()
		if controller != nil {
			controller.DeleteIf(uid, r)
		}
		return false
	}
	if r.State == StateWrong {
		r.mu.Unlock()
		return false
	}
	if r.DisconReason == InventoryRejected {
		r.rejectInventoryLoginUnsafe()
		r.mu.Unlock()
		return false
	}

	if r.State == StateRun || r.State == StateLogin || r.State == StateInit {
		r.logPartyTransportClearedUnsafe("session reconnect")
		r.stopPartySupervisorUnsafe()
		r.recvBuffer = nil
		r.recvSize = 0

		db := r.DB
		loginInfo := r.LoginInfo
		runStartTime := r.RunStartTime
		connCount := r.ConnCount + 1
		pendingStoreTitle := r.PendingStoreTitle
		afterRunDisjointCost := r.AfterRunDisjointCost

		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop
		r.publishSnapshotUnsafe()
		r.mu.Unlock()

		if connCount < loginInfo.MaxReConn && controller != nil {
			newVo := NewRobotVo(db)
			newVo.Load(loginInfo)
			newVo.mu.Lock()
			newVo.RunStartTime = runStartTime
			newVo.ConnCount = connCount
			newVo.PendingStoreTitle = pendingStoreTitle
			newVo.AfterRunDisjointCost = afterRunDisjointCost
			newVo.mu.Unlock()
			if !newVo.prepareConnect(controller) || !controller.replaceCurrent(uid, r, newVo) {
				newVo.CloseOut()
				return true
			}
			delaySec := int(newVo.ReDelay)
			if delaySec <= 0 {
				delaySec = 5
			} else {
				delaySec = int((time.Duration(newVo.ReDelay)*time.Millisecond + time.Second - 1) / time.Second)
			}
			controller.AddMessageDelay("MsgReconnect", newVo, delaySec)
		} else if controller != nil {
			controller.DeleteIf(uid, r)
		}
		return true
	}

	r.State = StateStop
	r.logPartyTransportClearedUnsafe("refresh invalid state")
	r.stopPartySupervisorUnsafe()
	if r.Conn != nil {
		r.Conn.Close()
		r.Conn = nil
	}
	r.closePartyUDPUnsafe()
	r.closePartyRelayUnsafe()
	r.publishSnapshotUnsafe()
	r.mu.Unlock()
	if controller != nil {
		controller.DeleteIf(uid, r)
	}
	return true
}
