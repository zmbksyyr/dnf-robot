package dnf

import (
	"fmt"
	"net"
	"time"
)

const (
	partySupervisorTick        = 100 * time.Millisecond
	partySupervisorMaintenance = time.Second
	partySelfRefreshInitial    = 3 * time.Second
	partySelfRefreshMax        = 30 * time.Second
)

func (r *RobotVo) ensurePartySupervisorUnsafe() bool {
	if r.partySupervisorRun || r.State != StateRun || !r.partyActiveUnsafe() {
		return false
	}
	r.partySupervisorEpoch++
	epoch := r.partySupervisorEpoch
	r.partySupervisorRun = true
	go r.partySupervisorLoop(epoch, r.UID)
	return true
}

func (r *RobotVo) stopPartySupervisorUnsafe() {
	r.partySupervisorEpoch++
	r.partySupervisorRun = false
}

func (r *RobotVo) partySupervisorLoop(epoch uint64, uid uint32) {
	ticker := time.NewTicker(partySupervisorTick)
	defer ticker.Stop()
	defer func() {
		if rec := recover(); rec != nil {
			r.mu.Lock()
			if r.partySupervisorEpoch == epoch {
				r.partySupervisorRun = false
			}
			r.mu.Unlock()
			fmt.Printf("[PARTY_SUPERVISOR_PANIC] uid=%d err=%v\n", uid, rec)
		}
	}()
	var nextMaintenance time.Time
	for now := range ticker.C {
		if !r.partySupervisorStep(epoch, uid, now, &nextMaintenance) {
			return
		}
	}
}

func (r *RobotVo) partySupervisorStep(epoch uint64, uid uint32, now time.Time, nextMaintenance *time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.State == StateRun && r.partySupervisorEpoch == epoch && r.partyActiveUnsafe()
	if !active {
		if r.partySupervisorEpoch == epoch {
			r.partySupervisorRun = false
		}
		return false
	}
	if nextMaintenance.IsZero() || !now.Before(*nextMaintenance) {
		r.ensurePartyUDPHealthUnsafe(uid)
		r.refreshPartySelfIdentityUnsafe(now)
		r.ensurePartyRelayUnsafe()
		r.startPartyRobotPeerNegotiationUnsafe()
		r.probePartyRobotPeerHealthUnsafe(r.partyUDPConn, now)
		*nextMaintenance = now.Add(partySupervisorMaintenance)
	}
	var udp *net.UDPConn
	if r.partyUDPRunning {
		udp = r.partyUDPConn
	}
	r.flushPartyRuntimeUnsafe(udp, now)
	return true
}

func (r *RobotVo) refreshPartySelfIdentityUnsafe(now time.Time) {
	self := r.partySelfPeer
	if self.uniqueID != 0 || self.accID != r.UID || !self.slotKnown || r.partyUDPConn == nil {
		return
	}
	if !r.partySelfRefreshAt.IsZero() && now.Before(r.partySelfRefreshAt) {
		return
	}
	if r.sendNATInfoUpdateUnsafe(true) {
		fmt.Printf("[PARTY_SELF_ID_REFRESH] uid=%d slot=%d\n", r.UID, self.slot)
	}
	if r.partySelfRefreshBackoff <= 0 {
		r.partySelfRefreshBackoff = partySelfRefreshInitial
	} else {
		r.partySelfRefreshBackoff *= 2
		if r.partySelfRefreshBackoff > partySelfRefreshMax {
			r.partySelfRefreshBackoff = partySelfRefreshMax
		}
	}
	r.partySelfRefreshAt = now.Add(r.partySelfRefreshBackoff)
}

func (r *RobotVo) ensurePartyUDPHealthUnsafe(uid uint32) {
	if r.State != StateRun || r.Conn == nil || !r.partyActiveUnsafe() {
		return
	}
	if r.partyUDPConn != nil && r.partyUDPRunning {
		return
	}
	addr, ok := r.Conn.LocalAddr().(*net.TCPAddr)
	if !ok || addr == nil {
		return
	}
	if r.partyUDPConn != nil {
		r.closePartyUDPUnsafe()
	}
	for slot := byte(0); slot < 4; slot++ {
		r.resetPartyTQOSRouteUnsafe(slot, 1)
		r.partyRouteBlockedUntil[slot][1] = time.Time{}
		r.partyRouteFailures[slot][1] = 0
	}
	r.natInfoSent = false
	if r.sendNATInfoUnsafe() {
		fmt.Printf("[PARTY_UDP_RECOVERED] uid=%d\n", uid)
	}
}
