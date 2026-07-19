package dnf

import "time"

func (r *RobotVo) CheckUserState() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateStop {
		return false
	}
	if r.DisconReason != NoDisconnect {
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
		r.ensurePartyUDPLoopUnsafe()
		r.ensurePartyRelayUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()
	} else if r.partyRelayConn != nil {
		r.closePartyRelayUnsafe()
	}
	return true
}

func (r *RobotVo) RefishConnect() bool {
	r.mu.Lock()
	controller := r.Controller
	uid := r.UID

	if r.State == StateStop {
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

	if r.State == StateRun || r.State == StateLogin || r.State == StateInit {
		r.recvBuffer = nil
		r.recvSize = 0

		db := r.DB
		loginInfo := r.LoginInfo
		runStartTime := r.RunStartTime
		connCount := r.ConnCount + 1
		tasks := append([]AsyncTask(nil), r.AfterRunAsyncTaskVec...)
		pendingStoreTitle := r.PendingStoreTitle

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
			newVo.AfterRunAsyncTaskVec = tasks
			newVo.PendingStoreTitle = pendingStoreTitle
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
