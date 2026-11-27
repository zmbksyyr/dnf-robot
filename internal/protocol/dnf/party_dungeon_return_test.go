package dnf

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestDungeonEmptyPartySnapshotRequestsTownBeforeClearingTransport(t *testing.T) {
	for _, humanObserved := range []bool{false, true} {
		t.Run(fmt.Sprint(humanObserved), func(t *testing.T) {
			r := NewRobotVo(nil)
			r.UID, r.State, r.Cipher = 17000061, StateRun, newPartyTestCipher(t)
			r.partyHumanObserved = humanObserved
			r.partySelfPeer = partyIPPeer{uniqueID: 422, accID: r.UID, slot: 1, slotKnown: true}
			r.partyPeers[0] = partyIPPeer{uniqueID: 421, accID: 17000062, slot: 0, slotKnown: true}
			conn := &captureSessionConn{}
			r.Conn, r.PacketID = conn, 17
			r.markPartyDungeonEnteredUnsafe(time.Now())
			// The server keeps only self after the other member leaves. The
			// leader's registered account type must not change this behavior.
			body := make([]byte, 23)
			body[0] = 1
			putPartyPeer(body[1:], 422, net.IPv4(127, 0, 0, 1), 5065, r.UID, 1, 1472)
			raw := makeEncryptedPartyRecvPacket(t, r.Cipher, 11, padPartyBlock(body))
			r.parsePacket(raw)
			if len(conn.written) != 13 || binary.LittleEndian.Uint16(conn.written[1:3]) != 45 || r.PacketID != 18 {
				t.Fatalf("expected empty command 45, got %x packetID=%d", conn.written, r.PacketID)
			}
			if !r.partyDungeonInMap || !r.partyDungeonReturnPending || !r.partyDungeonEnteredAt.IsZero() || r.partySupervisorRun || r.ReturnSelectPending {
				t.Fatal("party clear lost pending town return or kept combat running")
			}
			r.parsePacket(raw)
			r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 29, mustPartyHex(t, capturedRoomBody)))
			if len(conn.written) != 13 || r.partyDungeonLoading {
				t.Fatal("duplicate roster or late room notice restarted the dungeon")
			}
			r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 36, mustPartyHex(t, "0d009c000d000000eebf3d9e00000000")))
			if r.partyDungeonInMap || r.partyDungeonReturnPending || r.partyActiveUnsafe() {
				t.Fatal("server return notice did not finish the return")
			}
		})
	}
}

func TestDungeonRewardScreenStillReturnsWhenPartyEnds(t *testing.T) {
	r := NewRobotVo(nil)
	r.State, r.Cipher, r.Conn = StateRun, newPartyTestCipher(t), &captureSessionConn{}
	r.markPartyDungeonEnteredUnsafe(time.Now())
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 31, nil))
	r.setPartyPeersUnsafe(nil)
	if !r.partyDungeonReturnPending {
		t.Fatal("combat completion was mistaken for returning to town")
	}
	r.confirmPartyReturnTownUnsafe()
	conn := r.Conn.(*captureSessionConn)
	conn.written = nil
	r.setPartyPeersUnsafe(nil)
	if len(conn.written) != 0 {
		t.Fatal("an empty party in town sent another retreat")
	}
}

func TestDungeonTownNoticeOnlyConfirmsOwnReturn(t *testing.T) {
	r := NewRobotVo(nil)
	conn := &captureSessionConn{}
	r.State, r.Cipher, r.Conn = StateRun, newPartyTestCipher(t), conn
	r.characterUniqueID = 422
	r.partySelfPeer = partyIPPeer{uniqueID: 422, slot: 1, slotKnown: true}
	r.partyPeers[0] = partyIPPeer{uniqueID: 421, slot: 0, slotKnown: true}
	r.markPartyDungeonEnteredUnsafe(time.Now())
	r.observePartyTownReturnUnsafe(400)
	if !r.partyDungeonInMap || len(conn.written) != 0 {
		t.Fatal("another town entity changed dungeon membership")
	}
	r.observePartyTownReturnUnsafe(421)
	if !r.partyDungeonInMap || !r.partyDungeonReturnPending || len(conn.written) != 13 {
		t.Fatal("leader return did not request our own retreat")
	}
	// The empty snapshot has already discarded partySelfPeer; character
	// identity still recognizes the server's own town-area notification.
	r.partySelfPeer = partyIPPeer{}
	body := []byte{0xa6, 1, 3, 0, 0x64, 0, 0xc8, 0}
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 23, padPartyBlock(body)))
	if r.partyDungeonInMap || r.partyDungeonReturnPending {
		t.Fatal("own town-area notice did not confirm the return")
	}
}
