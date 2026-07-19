package dnf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	partyReliableWindow          = 4
	partyReliableRetryInitial    = 500 * time.Millisecond
	partyReliableRetryMax        = 5 * time.Second
	partyReliableRecoverAfter    = 12 * time.Second
	partyReliableRecoverCooldown = 30 * time.Second
)

var errPartyReliableBackpressure = errors.New("party reliable transport is backpressured")

type partyReliablePending struct {
	packet      []byte
	sequence    uint32
	firstSentAt time.Time
	nextRetry   time.Time
	retryDelay  time.Duration
	retries     uint16
	purpose     string
}

func (r *RobotVo) sendPartyReliableUnsafe(conn *net.UDPConn, peer partyIPPeer, flags byte, records [][]byte, purpose string, now time.Time) (string, error) {
	if !peer.slotKnown || peer.slot >= 4 || len(records) == 0 {
		return "", fmt.Errorf("party reliable peer or records are unavailable")
	}
	route := r.partyRouteForPeerUnsafe(peer.slot)
	destination, err := r.sendPartyReliableRouteUnsafe(conn, peer, route, flags, records, purpose, now)
	if err == nil {
		return destination, nil
	}
	alternate := r.partyAlternativeRouteUnsafe(peer, route, now)
	if alternate == 0 {
		return destination, err
	}
	return r.sendPartyReliableRouteUnsafe(conn, peer, alternate, flags, records, purpose, now)
}

func (r *RobotVo) sendPartyReliableRouteUnsafe(conn *net.UDPConn, peer partyIPPeer, route, flags byte, records [][]byte, purpose string, now time.Time) (string, error) {
	if route < 1 || route > 2 || !peer.slotKnown || peer.slot >= 4 {
		return "", fmt.Errorf("invalid party reliable route %d", route)
	}
	if !r.partyRouteSendEligibleUnsafe(conn, peer, route, now) {
		return "", errPartyReliableBackpressure
	}
	pending := &r.partyReliablePending[peer.slot][route]
	if len(*pending) >= partyReliableWindow {
		return "", errPartyReliableBackpressure
	}
	sequence := r.partyTQOSReliableSeq[peer.slot][route]
	packet := buildPartyReliablePacket(sequence, r.partySelfPeer.slot, flags, records)
	destination, err := r.sendPartyTransportUnsafe(conn, peer, route, packet)
	if err != nil {
		return destination, err
	}
	r.partyTQOSReliableSeq[peer.slot][route]++
	*pending = append(*pending, partyReliablePending{
		packet:      append([]byte(nil), packet...),
		sequence:    sequence,
		firstSentAt: now,
		nextRetry:   now.Add(partyReliableRetryInitial),
		retryDelay:  partyReliableRetryInitial,
		purpose:     purpose,
	})
	return destination, nil
}

func (r *RobotVo) flushPartyReliableUnsafe(conn *net.UDPConn, now time.Time) {
	for slot := byte(0); slot < 4; slot++ {
		peer := r.partyPeerForSlotUnsafe(slot)
		if !partyPeerIdentityKnown(peer) {
			continue
		}
		for route := byte(1); route <= 2; route++ {
			pending := r.partyReliablePending[slot][route]
			if len(pending) == 0 {
				continue
			}
			oldest := pending[0]
			lastRecovery := r.partyRouteRecoveryAt[slot][route]
			if now.Sub(oldest.firstSentAt) >= partyReliableRecoverAfter && (lastRecovery.IsZero() || now.Sub(lastRecovery) >= partyReliableRecoverCooldown) {
				r.recoverPartyRouteUnsafe(conn, peer, route, now, oldest)
				continue
			}
			for i := range pending {
				item := &pending[i]
				if item.nextRetry.IsZero() || now.Before(item.nextRetry) {
					continue
				}
				if _, err := r.sendPartyTransportUnsafe(conn, peer, route, item.packet); err == nil {
					item.retries++
				}
				item.retryDelay *= 2
				if item.retryDelay > partyReliableRetryMax {
					item.retryDelay = partyReliableRetryMax
				}
				item.nextRetry = now.Add(item.retryDelay)
			}
			r.partyReliablePending[slot][route] = pending
		}
	}
}

func (r *RobotVo) acknowledgePartyReliableUnsafe(frame []byte, route byte, peer partyIPPeer, now time.Time) bool {
	if len(frame) != 8 || frame[0] != 0 || !peer.slotKnown || peer.slot >= 4 || route < 1 || route > 2 || frame[1] != peer.slot {
		return false
	}
	sequence := binary.LittleEndian.Uint32(frame[2:6]) - 1
	pending := r.partyReliablePending[peer.slot][route]
	for i := range pending {
		if pending[i].sequence != sequence {
			continue
		}
		copy(pending[i:], pending[i+1:])
		pending[len(pending)-1] = partyReliablePending{}
		r.partyReliablePending[peer.slot][route] = pending[:len(pending)-1]
		r.markPartyRouteHealthyUnsafe(peer, route, now)
		return true
	}
	return false
}

func (r *RobotVo) recoverPartyRouteUnsafe(conn *net.UDPConn, peer partyIPPeer, route byte, now time.Time, stalled partyReliablePending) {
	r.partyRouteRecoveryAt[peer.slot][route] = now
	if strings.HasPrefix(stalled.purpose, "skill-") {
		r.partySkillBlockedUntil = now.Add(partySkillFailureCooldown)
	}
	r.markPartyRouteFailureUnsafe(peer, route, now, "reliable ack stalled")
	r.resetPartyTQOSRouteUnsafe(peer.slot, route)
	sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
	if !ok {
		return
	}
	packet := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, 0, 3, route, partyTQOSCodec{key: 0x7e})
	destination, err := r.sendPartyTransportUnsafe(conn, peer, route, packet)
	if err != nil {
		fmt.Printf("[PARTY_ROUTE_RECOVERY_ERROR] uid=%d peer=%d slot=%d route=%d purpose=%s destination=%s err=%v\n",
			r.UID, peer.accID, peer.slot, route, stalled.purpose, destination, err)
		return
	}
	fmt.Printf("[PARTY_ROUTE_RECOVERY] uid=%d peer=%d slot=%d route=%d stalled_sequence=%d age=%s retries=%d purpose=%s\n",
		r.UID, peer.accID, peer.slot, route, stalled.sequence, now.Sub(stalled.firstSentAt).Round(time.Millisecond), stalled.retries, stalled.purpose)
}

func (r *RobotVo) resetPartyTQOSRouteUnsafe(slot, route byte) {
	if slot >= 4 || route < 1 || route > 2 {
		return
	}
	r.partyTQOSSeq[slot][route] = 0
	r.partyTQOSReliableSeq[slot][route] = 0
	r.partyTQOSReplies[slot][route] = partyTQOSReliableReply{}
	r.partyReliablePending[slot][route] = nil
	r.partyTQOSReceived[slot][route] = partyTQOSReceiveWindow{}
	r.partyTQOSEpochs[slot][route] = partyTQOSEpoch{}
	r.partyTQOSCodecs[slot][route] = partyTQOSCodec{}
	r.partyTQOSCodecKnown[slot][route] = false
	r.clearPartyRobotRouteReadyUnsafe(slot, route)
	r.partyRouteActivityAt[slot][route] = time.Time{}
}

func (r *RobotVo) partyReliablePendingCountUnsafe(slot byte) int {
	if slot >= 4 {
		return 0
	}
	total := 0
	for route := byte(1); route <= 2; route++ {
		total += len(r.partyReliablePending[slot][route])
	}
	return total
}

func (r *RobotVo) partyReliablePurposePendingUnsafe(slot byte, prefix string) bool {
	if slot >= 4 {
		return false
	}
	for route := byte(1); route <= 2; route++ {
		for _, pending := range r.partyReliablePending[slot][route] {
			if strings.HasPrefix(pending.purpose, prefix) {
				return true
			}
		}
	}
	return false
}

func (r *RobotVo) cancelPartyDungeonReliableUnsafe(now time.Time) {
	for slot := byte(0); slot < 4; slot++ {
		peer := r.partyPeerForSlotUnsafe(slot)
		if !partyPeerIdentityKnown(peer) {
			continue
		}
		for route := byte(1); route <= 2; route++ {
			cancel := false
			for _, pending := range r.partyReliablePending[slot][route] {
				if pending.purpose == "follow" || strings.HasPrefix(pending.purpose, "skill-") {
					cancel = true
					break
				}
			}
			if !cancel {
				continue
			}
			r.resetPartyTQOSRouteUnsafe(slot, route)
			if !r.partyRouteAvailableUnsafe(peer, route) {
				continue
			}
			sequence, ok := r.nextPartyTQOSSequenceUnsafe(slot, route, false)
			if !ok {
				continue
			}
			packet := buildPartyTQOSPacket(sequence, r.partySelfPeer.slot, 0, 3, route, partyTQOSCodec{key: 0x7e})
			_, _ = r.sendPartyTransportUnsafe(r.partyUDPConn, peer, route, packet)
		}
	}
}
