package dnf

import (
	"net"
	"testing"
	"time"
)

func TestRoomEntryDoesNotReplayOldDeltaOrStandInReturnGate(t *testing.T) {
	// Windows 2026-11-22 18:11:33: an old-room unreliable delta arrived
	// during loading, followed by a reliable full position in the new room.
	// Replaying either the old delta or the exact entrance caused a return.
	now := time.Unix(100, 0)
	r := &RobotVo{UID: 17000015,
		partySelfPeer:         partyIPPeer{uniqueID: 1, slot: 1, slotKnown: true},
		partyDungeonEnteredAt: now, partyDungeonLastAt: now,
	}
	r.partyPeers[0] = partyIPPeer{uniqueID: 2, slot: 0, slotKnown: true}
	peer := r.partyPeers[0]
	r.partyDungeonWorld.observe(mustPartyHex(t, "0104002e323dc77f6f7f7f7e6f7f7f7e7f7d6e2b2b3f16f9d13d096b683d"), now)
	r.partyDungeonWorld.observe(mustPartyHex(t, "010400a26240e07f6f7f7c7e6f7f7c7e7f7d6e2b2b3fcd4b033d7e7e6b3d"), now)
	r.markPartyDungeonEnteredUnsafe(now.Add(10 * time.Millisecond))
	sender, receiver := comboTestTransport(t, r)
	old := mustPartyHex(t, "02c60300001e000000010400728156e07f6f7f7c7e6f7f7c7e7f7d7e2b6b37b4e46a3d71d3bbbc")
	fresh := mustPartyHex(t, "01f75b00002d0000000b00014f00280bcff16f7f7c7e1e00010400361b6f8f7f6f7f7c7e6f7f7c7e7f7c6e2b2b3f7efe313a7e7ed43d")
	for i, frame := range [][]byte{old, fresh} {
		at := now.Add(time.Duration(46+i*554) * time.Millisecond)
		r.observePartyDungeonFrameUnsafe(frame, peer, at)
		if !r.queuePartyDungeonFollowUnsafe(frame, peer, at) {
			t.Fatal("captured motion not queued")
		}
		if i == 0 {
			// A complete old-room update must not bootstrap the new room either.
			r.partyDungeonWorld.observe(mustPartyHex(t, "010400a26240e07f6f7f7c7e6f7f7c7e7f7d6e2b2b3fcd4b033d7e7e6b3d"), at)
			r.partyDungeonLoading = false
			r.flushPartyDungeonFollowUnsafe(sender, now.Add(500*time.Millisecond))
			receiver.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
			if _, _, err := receiver.ReadFromUDP(make([]byte, 1024)); err == nil {
				t.Fatal("old room motion was replayed after loading")
			} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
				t.Fatal(err)
			}
		}
	}
	r.flushPartyDungeonFollowUnsafe(sender, now.Add(time.Second))
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	frames, ok := partyDungeonFrameBodies(buf[:n])
	if !ok || len(frames) != 1 {
		t.Fatalf("entry frame: %x", buf[:n])
	}
	entries, ok := decodePartyDungeonMotion(frames[0])
	if !ok || len(entries) != 1 {
		t.Fatal("entry motion missing")
	}
	entry := entries[0]
	position := (partyDungeonPosition{}).apply(entry)
	if entry.entity != partyDungeonPlayerReference(1) || entry.controller != entry.entity || entry.flags[0] != 1 || entry.flags[1] != 2 || position.x != 830 || position.y != 260 || position.known != 7 {
		t.Fatalf("entry did not stage inside the new room: %+v %+v", entry, position)
	}
	if len(r.partyDungeonFollow) != 0 {
		t.Fatal("entry left stale leader replay queued")
	}
}

func TestRoomEntryStagesInwardInEachDirectionAndWaitsForLoading(t *testing.T) {
	for _, tc := range []struct {
		name         string
		room         [2]byte
		x, y         float32
		wantX, wantY float32
	}{
		{"up", [2]byte{1, 0}, 830, 340, 830, 260},
		{"down", [2]byte{1, 2}, 830, 150, 830, 230},
		{"left", [2]byte{0, 1}, 1500, 240, 1420, 240},
		{"right", [2]byte{2, 1}, 50, 240, 130, 240},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			world, self, leader, enemy := navigationTestWorld(now)
			delete(world.actors, enemy)
			actor := world.actors[self]
			actor.room = [2]byte{1, 1}
			world.actors[self] = actor
			actor = world.actors[leader]
			actor.room, actor.position.x, actor.position.y = tc.room, tc.x, tc.y
			world.actors[leader] = actor
			// New-room UDP can beat the server notification. It must not
			// relabel the old position or start chasing in that room yet.
			if _, move, active := world.planNavigation(self, leader, now); move || !active {
				t.Fatal("peer room changed before server loading")
			}
			r := &RobotVo{partyDungeonWorld: world, partySelfPeer: partyIPPeer{uniqueID: 5}}
			r.markPartyDungeonEnteredUnsafe(now)
			// START_GAME and START_MAP may both reset an empty world.
			r.markPartyDungeonEnteredUnsafe(now)
			r.partyDungeonWorld.actors = map[[4]byte]partyDungeonActor{leader: actor}
			position, move, active := r.partyDungeonWorld.planNavigation(self, leader, now)
			if !move || !active || position.x != tc.wantX || position.y != tc.wantY || position.known != 7 {
				t.Fatalf("did not initialize inside %s entrance: %+v", tc.name, position)
			}
		})
	}
}

func TestRoomMotionDoesNotRetainCoordinatesAcrossRoomBoundary(t *testing.T) {
	now := time.Unix(100, 0)
	world := partyDungeonWorld{}
	world.observe(mustPartyHex(t, "010400361b6f8f7f6f7f7c7e6f7f7c7e7f7c6e2b2b3f7efe313a7e7ed43d"), now)
	world.observe(mustPartyHex(t, "010400728156e07f6f7f7c7e6f7f7c7e7f7d7e2b6b37b4e46a3d71d3bbbc"), now.Add(time.Millisecond))
	position := world.actors[partyDungeonPlayerReference(2)].position
	if position.known&1 != 0 {
		t.Fatalf("old-room retain inherited the new room's x: %+v", position)
	}
}
