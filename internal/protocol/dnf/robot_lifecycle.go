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
	defer r.mu.Unlock()

	if r.State == StateStop {
		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		if r.Controller != nil {
			r.Controller.Delete(r.UID)
		}
		return false
	}

	if r.State == StateRun || r.State == StateLogin || r.State == StateInit {
		r.recvBuffer = nil
		r.recvSize = 0

		newVo := NewRobotVo(r.DB)
		newVo.Controller = r.Controller
		newVo.Load(r.LoginInfo)
		newVo.RunStartTime = r.RunStartTime
		newVo.ConnCount = r.ConnCount + 1
		newVo.AfterRunAsyncTaskVec = r.AfterRunAsyncTaskVec
		newVo.PendingStoreTitle = r.PendingStoreTitle

		if r.Conn != nil {
			r.Conn.Close()
			r.Conn = nil
		}
		r.closePartyUDPUnsafe()
		r.closePartyRelayUnsafe()
		r.State = StateStop

		if newVo.ConnCount < newVo.MaxReConn {
			if r.Controller != nil {
				r.Controller.Delete(r.UID)
			}
			delaySec := int(newVo.ReDelay)
			if delaySec <= 0 {
				delaySec = 5
			} else {
				delaySec = int((time.Duration(newVo.ReDelay)*time.Millisecond + time.Second - 1) / time.Second)
			}
			if r.Controller != nil {
				r.Controller.AddMessageDelay("MsgOnLine", newVo, delaySec)
			}
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
	return true
}
