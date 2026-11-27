package dnf

import (
	"encoding/binary"
	"math"
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
	return r.partySelfPeer.slotKnown && r.partySelfPeer.slot != 0 && r.partySelfPeer.uniqueID != 0 && peer.slotKnown && peer.slot == 0 && peer.uniqueID != 0
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
	// Coalescing can replace the room's first full position with a later
	// retain-only update before either is sent. Carry the observed leader
	// coordinates into retained position fields so the follower can initialize
	// its own position even when that first update was superseded.
	position := r.partyDungeonWorld.actors[partyDungeonPlayerReference(peer.uniqueID)].position
	if pending.reliable {
		for i, body := range pending.records {
			pending.records[i] = snapshotPartyDungeonFollowMotion(body, position)
		}
	} else {
		pending.body = snapshotPartyDungeonFollowMotion(pending.body, position)
	}
	if len(r.partyDungeonFollow) >= 2048 {
		r.partyDungeonFollow = nil
		return false
	}
	for i := range r.partyDungeonFollow {
		existing := &r.partyDungeonFollow[i]
		if existing.peerSlot == pending.peerSlot && existing.reliable == pending.reliable {
			existing.flags = pending.flags
			existing.body = pending.body
			existing.records = pending.records
			return true
		}
	}
	r.partyDungeonFollow = append(r.partyDungeonFollow, pending)
	return true
}

func snapshotPartyDungeonFollowMotion(body []byte, position partyDungeonPosition) []byte {
	entries, ok := decodePartyDungeonMotion(body)
	if !ok {
		return body
	}
	changed := false
	for i := range entries {
		for axis, value := range [...]float32{position.x, position.y, position.z} {
			field := 9 + axis
			if position.known&(1<<axis) != 0 && entries[i].mode(field) == 2 {
				entries[i].modes &^= 3 << uint(2*field)
				entries[i].values[field] = math.Float32bits(value)
				changed = true
			}
		}
	}
	if changed {
		if snapshot, valid := buildPartyDungeonMotion(entries); valid {
			return snapshot
		}
	}
	return body
}

func (r *RobotVo) partyDungeonFollowDelayUnsafe() time.Duration {
	return time.Duration(300+int(r.UID%701)) * time.Millisecond
}

func (r *RobotVo) flushPartyDungeonFollowUnsafe(conn *net.UDPConn, now time.Time) {
	// Peer state can arrive during loading. Retain it, but do not act on the
	// room until the server confirms every member has finished loading.
	if r.partyDungeonLoading || !r.partyDungeonWorld.playerCanAct(r.partySelfPeer.uniqueID) {
		return
	}
	// Replayed leader states can cancel our own cast. Keep the latest position
	// queued until the cast and its recovery have both been acknowledged.
	if !r.partySkillRecoverAt.IsZero() || r.partyReliablePurposePendingUnsafe(0, "skill-") {
		return
	}
	if r.navigatePartyDungeonUnsafe(conn, now) {
		return
	}
	for len(r.partyDungeonFollow) > 0 && !now.Before(r.partyDungeonFollow[0].due) {
		pending := r.partyDungeonFollow[0]
		peer := r.partyPeerForSlotUnsafe(pending.peerSlot)
		if peer.uniqueID == 0 {
			r.partyDungeonFollow = r.partyDungeonFollow[1:]
			continue
		}
		var err error
		route := byte(0)
		if pending.reliable {
			if r.partyReliableWindowFullUnsafe(peer.slot) {
				r.partyDungeonFollow[0].due = now.Add(500 * time.Millisecond)
				return
			}
			_, err = r.sendPartyReliableUnsafe(conn, peer, pending.flags, pending.records, "follow", now)
		} else {
			route = r.partyRouteForPeerUnsafe(peer.slot)
			if !r.partyRouteSendEligibleUnsafe(conn, peer, route, now) {
				err = errPartyReliableBackpressure
			} else {
				sequence, ok := r.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
				if !ok {
					err = errPartyReliableBackpressure
				} else {
					payload, buildErr := buildPartyUnreliablePacketChecked(sequence, r.partySelfPeer.slot, pending.flags, pending.body)
					if buildErr != nil {
						err = buildErr
					} else {
						_, err = r.sendPartyTransportUnsafe(conn, peer, route, payload)
					}
				}
			}
		}
		if err != nil {
			r.partyDungeonFollow[0].due = now.Add(500 * time.Millisecond)
			if r.shouldLogPartyRuntimeErrorUnsafe(now) {
				foundationlog.Robotf("[PARTY_DUNGEON_FOLLOW_ERROR] uid=%d peer=%d route=%d reliable=%t err=%v\n", r.UID, peer.accID, route, pending.reliable, err)
			}
			return
		}
		if !now.Before(r.partyDungeonFollowDiagAt) {
			r.partyDungeonFollowDiagAt = now.Add(5 * time.Second)
			foundationlog.Robotf("[PARTY_DUNGEON_FOLLOW] uid=%d peer=%d slot=%d route=%d reliable=%t queued=%d delay=%s\n", r.UID, peer.accID, pending.peerSlot, route, pending.reliable, len(r.partyDungeonFollow), r.partyDungeonFollowDelayUnsafe())
		}
		if pending.reliable {
			for _, body := range pending.records {
				r.partyDungeonWorld.observe(body, now)
			}
		} else {
			r.partyDungeonWorld.observe(pending.body, now)
		}
		r.partyDungeonFollow = r.partyDungeonFollow[1:]
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
