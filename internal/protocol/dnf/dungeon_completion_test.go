package dnf

import (
	"encoding/binary"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestDungeonCompletionStopsActionsAndRetainsParty(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.State, r.Cipher, r.Conn = StateRun, newPartyTestCipher(t), &captureSessionConn{}
	sender, receiver := comboTestTransport(t, r)
	if !r.tryPartyBasicAttackUnsafe(sender, now) {
		t.Fatal("attack setup did not start a pending combo")
	}
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := receiver.ReadFromUDP(buf); err != nil {
		t.Fatal(err)
	}
	peer := r.partyPeers[0]
	// A rejected or truncated completion must not cancel an active dungeon.
	r.handlePartyPacketUnsafe(robotInboundPacket{typ: 31, flag: 1, size: 15})
	r.parsePacket([]byte{0, 31, 0, 7, 0, 0, 0})
	if r.partyDungeonEnteredAt.IsZero() {
		t.Fatal("invalid completion ended combat")
	}
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 31, nil))
	if !r.partyDungeonEnteredAt.IsZero() || len(r.partyDungeonWorld.actors) != 0 || r.partyBasicAttackStep != 0 || !r.partySkillRecoverAt.IsZero() {
		t.Fatal("completion retained dungeon actions or actors")
	}
	if !reflect.DeepEqual(r.partyPeers[0], peer) {
		t.Fatal("completion changed the party roster")
	}
	// Late real-client position packets still get their transport ACK, but must
	// not revive the world, queued movement, casts, or the previous attack retry.
	body := mustPartyHex(t, "010400bbc431877f6f7f7c7e6f7f7c7e7e7e4e2b2b3f7efe983d7efed43d")
	replies := r.buildPartyTQOSRepliesUnsafe(buildPartyReliablePacket(1, 0, 0, [][]byte{body}), 1, peer)
	if len(replies) != 1 {
		t.Fatal("completion stopped transport acknowledgements")
	}
	r.flushPartyRuntimeUnsafe(sender, now.Add(3*time.Second))
	receiver.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	for {
		n, _, err := receiver.ReadFromUDP(buf)
		if err != nil {
			if e, ok := err.(net.Error); !ok || !e.Timeout() {
				t.Fatal(err)
			}
			break
		}
		bodies, ok := partyDungeonFrameBodies(buf[:n])
		if !ok || len(bodies) != 0 {
			t.Fatal("dungeon action or retry transmitted after completion")
		}
		r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, binary.LittleEndian.Uint32(buf[1:5])), 1, peer)
	}
	if len(r.partyDungeonWorld.actors) != 0 || len(r.partyDungeonFollow) != 0 {
		t.Fatal("late position revived the completed dungeon")
	}
	// A subsequent entry may start again through the ordinary loading gate.
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 29, mustPartyHex(t, capturedRoomBody)))
	if r.partyDungeonEnteredAt.IsZero() || !r.partyDungeonLoading {
		t.Fatal("next dungeon did not enter loading")
	}
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 30, make([]byte, 8)))
	if r.partyDungeonLoading {
		t.Fatal("next dungeon did not start normally")
	}
}
