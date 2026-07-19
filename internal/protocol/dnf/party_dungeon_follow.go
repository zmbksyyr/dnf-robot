package dnf

import (
	"encoding/binary"
	"net"
	"time"

	foundationlog "robot/internal/foundation/log"
)

type partyDungeonFollowPending struct {
	due      time.Time
	peerSlot byte
	flags    byte
	reliable bool
	body     []byte
	records  [][]byte
}

func (r *RobotVo) shouldFollowPartyPeerUnsafe(peer partyIPPeer) bool {
	return r.partySelfPeer.slotKnown && r.partySelfPeer.slot != 0 && peer.slotKnown && peer.slot == 0 && peer.uniqueID != 0
}

func (r *RobotVo) queuePartyDungeonFollowUnsafe(frame []byte, peer partyIPPeer, now time.Time) bool {
	if len(frame) < 9 || !peer.slotKnown || peer.slot >= 4 {
		return false
	}
	if r.partyDungeonEnteredAt.IsZero() || now.Sub(r.partyDungeonEnteredAt) > partyDungeonEntryTimeout {
		return false
	}
	bodySize := int(binary.LittleEndian.Uint16(frame[5:7]))
	if len(frame) != 9+bodySize {
		return false
	}
	pending := partyDungeonFollowPending{
		due:      now.Add(r.partyDungeonFollowDelayUnsafe()),
		peerSlot: peer.slot,
		flags:    frame[8],
	}
	switch frame[0] {
	case 0x02:
		body, _, ok := rewritePartyDungeonBody(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
		if !ok {
			return false
		}
		pending.body = body
	case 0x01:
		records := rewritePartyDungeonRecords(frame[9:], peer.uniqueID, r.partySelfPeer.uniqueID)
		if len(records) == 0 {
			return false
		}
		pending.reliable = true
		pending.records = records
	default:
		return false
	}
	if len(r.partyDungeonFollow) >= 2048 {
		r.partyDungeonFollow = nil
		return false
	}
	r.partyDungeonFollow = append(r.partyDungeonFollow, pending)
	return true
}

func (r *RobotVo) partyDungeonFollowDelayUnsafe() time.Duration {
	return time.Duration(2000+int(r.UID%2001)) * time.Millisecond
}

func (r *RobotVo) flushPartyDungeonFollowUnsafe(conn *net.UDPConn, now time.Time) {
	for len(r.partyDungeonFollow) > 0 && !now.Before(r.partyDungeonFollow[0].due) {
		pending := r.partyDungeonFollow[0]
		r.partyDungeonFollow = r.partyDungeonFollow[1:]
		peer := r.partyPeerForSlotUnsafe(pending.peerSlot)
		if peer.uniqueID == 0 {
			continue
		}
		route := r.partyRouteForPeerUnsafe(peer.slot)
		sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, pending.reliable)
		if !ok {
			continue
		}
		var payload []byte
		if pending.reliable {
			payload = buildPartyReliablePacket(sequence, r.partySelfPeer.slot, pending.flags, pending.records)
		} else {
			payload = buildPartyUnreliablePacket(sequence, r.partySelfPeer.slot, pending.flags, pending.body)
		}
		if _, err := r.sendPartyTransportUnsafe(conn, peer, route, payload); err != nil {
			foundationlog.Robotf("[PARTY_DUNGEON_FOLLOW_ERROR] uid=%d peer=%d route=%d err=%v\n", r.UID, peer.accID, route, err)
		}
	}
}

func partyDungeonFrameContainsCommand(frame []byte, target uint16) bool {
	if len(frame) < 12 {
		return false
	}
	if frame[0] == 0x02 {
		return binary.LittleEndian.Uint16(frame[10:12]) == target
	}
	if frame[0] != 0x01 {
		return false
	}
	body := frame[9:]
	for len(body) >= 2 {
		size := int(binary.LittleEndian.Uint16(body[:2]))
		body = body[2:]
		if size <= 0 || size > len(body) {
			return false
		}
		if size >= 3 && binary.LittleEndian.Uint16(body[1:3]) == target {
			return true
		}
		body = body[size:]
	}
	return false
}
