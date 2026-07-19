package dnf

import (
	"fmt"
	"net"
	"time"
)

const (
	partyRouteFailureBase = 2 * time.Second
	partyRouteFailureMax  = 30 * time.Second
	partyRouteDiagGap     = 15 * time.Second
	partyRuntimeDiagGap   = 10 * time.Second
)

func (r *RobotVo) setPartyRobotRouteReadyUnsafe(peer partyIPPeer, route byte, ready bool, reason string, now time.Time) {
	if !peer.slotKnown || peer.slot >= 4 || route < 1 || route > 2 || !isPartyRobotAccount(peer.accID) {
		return
	}
	wasRouteReady := r.partyRobotRouteReady[peer.slot][route]
	wasPeerReady := r.partyRobotPeerReady[peer.slot]
	r.partyRobotRouteReady[peer.slot][route] = ready
	r.partyRobotPeerReady[peer.slot] = r.partyRobotRouteReady[peer.slot][1] || r.partyRobotRouteReady[peer.slot][2]
	if ready {
		r.partyPeerRoute[peer.slot] = route
		r.partyPeerRouteAt[peer.slot] = now
		r.partyRouteActivityAt[peer.slot][route] = now
		if !wasRouteReady {
			fmt.Printf("[PARTY_ROBOT_TQOS_READY] uid=%d peer=%d slot=%d route=%d reason=%s\n", r.UID, peer.accID, peer.slot, route, reason)
		}
		r.partyRobotProbeAt[peer.slot] = time.Time{}
		r.partyRobotProbeCount[peer.slot] = 0
		return
	}
	if wasPeerReady && !r.partyRobotPeerReady[peer.slot] {
		r.partyRobotProbeAt[peer.slot] = time.Time{}
		r.partyRobotProbeCount[peer.slot] = 0
	}
}

func (r *RobotVo) clearPartyRobotRouteReadyUnsafe(slot, route byte) {
	if slot >= 4 || route < 1 || route > 2 {
		return
	}
	wasPeerReady := r.partyRobotPeerReady[slot]
	r.partyRobotRouteReady[slot][route] = false
	r.partyRobotPeerReady[slot] = r.partyRobotRouteReady[slot][1] || r.partyRobotRouteReady[slot][2]
	if wasPeerReady && !r.partyRobotPeerReady[slot] {
		r.partyRobotProbeAt[slot] = time.Time{}
		r.partyRobotProbeCount[slot] = 0
	}
}

func (r *RobotVo) markPartyRouteFailureUnsafe(peer partyIPPeer, route byte, now time.Time, reason string) {
	if !peer.slotKnown || peer.slot >= 4 || route < 1 || route > 2 {
		return
	}
	failures := r.partyRouteFailures[peer.slot][route]
	if failures < 8 {
		failures++
	}
	r.partyRouteFailures[peer.slot][route] = failures
	delay := partyRouteFailureBase
	for step := uint8(1); step < failures && delay < partyRouteFailureMax; step++ {
		delay *= 2
	}
	if delay > partyRouteFailureMax {
		delay = partyRouteFailureMax
	}
	blockedUntil := now.Add(delay)
	if blockedUntil.After(r.partyRouteBlockedUntil[peer.slot][route]) {
		r.partyRouteBlockedUntil[peer.slot][route] = blockedUntil
	}
	if isPartyRobotAccount(peer.accID) {
		r.setPartyRobotRouteReadyUnsafe(peer, route, false, reason, now)
	}
	if now.Before(r.partyRouteDiagAt[peer.slot][route]) {
		return
	}
	r.partyRouteDiagAt[peer.slot][route] = now.Add(partyRouteDiagGap)
	fmt.Printf("[PARTY_ROUTE_DEGRADED] uid=%d peer=%d slot=%d route=%d failures=%d retry_in=%s reason=%s\n",
		r.UID, peer.accID, peer.slot, route, failures, delay, reason)
}

func (r *RobotVo) shouldLogPartyRuntimeErrorUnsafe(now time.Time) bool {
	if now.Before(r.partyRuntimeDiagAt) {
		return false
	}
	r.partyRuntimeDiagAt = now.Add(partyRuntimeDiagGap)
	return true
}

func (r *RobotVo) markPartyRouteHealthyUnsafe(peer partyIPPeer, route byte, now time.Time) {
	if !peer.slotKnown || peer.slot >= 4 || route < 1 || route > 2 {
		return
	}
	wasBlocked := !r.partyRouteBlockedUntil[peer.slot][route].IsZero()
	r.partyRouteFailures[peer.slot][route] = 0
	r.partyRouteBlockedUntil[peer.slot][route] = time.Time{}
	r.partyRouteActivityAt[peer.slot][route] = now
	r.partyPeerRoute[peer.slot] = route
	r.partyPeerRouteAt[peer.slot] = now
	if wasBlocked {
		fmt.Printf("[PARTY_ROUTE_RECOVERED] uid=%d peer=%d slot=%d route=%d\n", r.UID, peer.accID, peer.slot, route)
	}
}

func (r *RobotVo) partyRouteEligibleUnsafe(peer partyIPPeer, route byte, now time.Time) bool {
	if !peer.slotKnown || peer.slot >= 4 || !r.partyRouteAvailableUnsafe(peer, route) {
		return false
	}
	blockedUntil := r.partyRouteBlockedUntil[peer.slot][route]
	return blockedUntil.IsZero() || !now.Before(blockedUntil)
}

func (r *RobotVo) partyRouteSendEligibleUnsafe(conn *net.UDPConn, peer partyIPPeer, route byte, now time.Time) bool {
	if !peer.slotKnown || peer.slot >= 4 || route < 1 || route > 2 {
		return false
	}
	blockedUntil := r.partyRouteBlockedUntil[peer.slot][route]
	if !blockedUntil.IsZero() && now.Before(blockedUntil) {
		return false
	}
	if route == 1 {
		_, ok := partyPeerUDPAddr(peer)
		return ok && conn != nil
	}
	return r.partyRouteAvailableUnsafe(peer, route)
}

func (r *RobotVo) partyAlternativeRouteUnsafe(peer partyIPPeer, route byte, now time.Time) byte {
	for _, candidate := range []byte{1, 2} {
		if candidate != route && r.partyRouteEligibleUnsafe(peer, candidate, now) {
			return candidate
		}
	}
	return 0
}
