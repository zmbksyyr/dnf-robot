package dnf

import (
	"bytes"
	"math"
	"testing"
	"time"
)

func TestCoalescedFollowRetainsObservedRoomPosition(t *testing.T) {
	for _, reliable := range []bool{false, true} {
		t.Run(map[bool]string{false: "unreliable", true: "reliable"}[reliable], func(t *testing.T) {
			now := time.Unix(100, 0)
			peer := partyIPPeer{slot: 0, slotKnown: true, uniqueID: 7}
			r := &RobotVo{UID: 17000039, partySelfPeer: partyIPPeer{slot: 1, slotKnown: true, uniqueID: 6}}
			r.markPartyDungeonEnteredUnsafe(now)
			leader := partyDungeonPlayerReference(7)
			entry := partyDungeonMotion{entity: leader, controller: leader, modes: 0xaaaaaa}
			for axis, value := range []float32{675, 149, 0} {
				entry.modes &^= 3 << uint(2*(9+axis))
				entry.values[9+axis] = math.Float32bits(value)
			}
			for sequence := uint32(1); sequence <= 2; sequence++ {
				body, ok := buildPartyDungeonMotion([]partyDungeonMotion{entry})
				if !ok {
					t.Fatal("motion build failed")
				}
				frame := buildPartyUnreliablePacket(sequence, 0, 0, body)
				if reliable {
					frame = buildPartyReliablePacket(sequence, 0, 0, [][]byte{body})
				}
				// The receive path observes each frame before queuing its replay.
				r.observePartyDungeonFrameUnsafe(frame, peer, now)
				if !r.queuePartyDungeonFollowUnsafe(frame, peer, now) {
					t.Fatal("motion was not queued")
				}
				// The second frame updates x and retains y/z. It supersedes the
				// complete position before the follow delay expires.
				entry.values[9] = math.Float32bits(635)
				entry.modes |= 2<<20 | 2<<22
			}
			if len(r.partyDungeonFollow) != 1 {
				t.Fatalf("queue length = %d", len(r.partyDungeonFollow))
			}
			pending := r.partyDungeonFollow[0]
			body := pending.body
			if reliable {
				body = pending.records[0]
			}
			r.partyDungeonWorld.observe(body, now)
			position := r.partyDungeonWorld.actors[partyDungeonPlayerReference(6)].position
			if position.known != 7 || position.x != 635 || position.y != 149 || position.z != 0 {
				t.Fatalf("coalesced replay lost the room position: %+v", position)
			}
		})
	}
}

func TestFollowSnapshotPreservesUnknownPositionModes(t *testing.T) {
	self := partyDungeonPlayerReference(6)
	entry := partyDungeonMotion{entity: self, controller: self, modes: 0xaaaaaa}
	entry.modes = entry.modes&^(3<<22) | 3<<22 // z is explicitly unknown.
	body, _ := buildPartyDungeonMotion([]partyDungeonMotion{entry})
	if got := snapshotPartyDungeonFollowMotion(body, partyDungeonPosition{}); !bytes.Equal(got, body) {
		t.Fatal("unknown coordinates were filled in")
	}
	got := snapshotPartyDungeonFollowMotion(body, partyDungeonPosition{x: 675, known: 1})
	world := partyDungeonWorld{}
	world.observe(got, time.Unix(100, 0))
	position := world.actors[self].position
	if position.known != 1 || position.x != 675 {
		t.Fatalf("unknown y/z were invented: %+v", position)
	}
}
