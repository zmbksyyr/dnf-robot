package dnf

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestPartyReturnIgnoresLateDungeonNotifications(t *testing.T) {
	for _, transport := range []string{"encrypted", "plain", "anti"} {
		for _, typ := range []uint16{28, 29, 30, 31} {
			t.Run(fmt.Sprintf("%s/type%d", transport, typ), func(t *testing.T) {
				r := NewRobotVo(nil)
				r.Cipher, r.State = newPartyTestCipher(t), StateRun
				conn := &captureSessionConn{}
				r.Conn, r.PacketID = conn, 17
				r.markPartyDungeonEnteredUnsafe(time.Now().Add(-time.Second))
				r.partyDungeonIdentity = dungeonDefinitionIdentity{known: true, id: 9001}
				entered, last, next := r.partyDungeonEnteredAt, r.partyDungeonLastAt, r.partySkillNextAt
				if !r.sendReturnToCharacterSelectUnsafe(true) {
					t.Fatal("return request failed")
				}
				written := append([]byte(nil), conn.written...)
				body := make([]byte, 8)
				if typ == 28 {
					body = dungeonIdentityTestBody(4, 0)
				} else if typ == 29 {
					body = mustPartyHex(t, capturedRoomBody)
				} else if typ == 31 {
					body = make([]byte, 16)
				}
				raw := makePartyRecvPacket(typ, body)
				if transport == "encrypted" {
					if typ == 28 {
						body = append(body, make([]byte, alignTo(len(body)+4, 4)-len(body))...)
					}
					raw = makeEncryptedPartyRecvPacket(t, r.Cipher, typ, body)
				}
				dispatch := func() {
					if transport == "anti" {
						r.handlePartyPacketUnsafe(robotInboundPacket{typ: typ, data: raw, size: len(raw), isAnti: true})
					} else {
						r.parsePacket(raw)
					}
				}
				dispatch()
				if !bytes.Equal(written, conn.written) || r.PacketID != 18 ||
					r.partyDungeonEnteredAt != entered || r.partyDungeonLastAt != last || r.partySkillNextAt != next ||
					!r.partyDungeonLoading || r.partyDungeonIdentity.id != 9001 || !r.ReturnSelectPending {
					t.Fatal("pending return accepted late dungeon notification")
				}
				// A rejected command 7 leaves the old character active. The same
				// notification must become usable again without reconnecting.
				rejectPartyReturn(t, r)
				dispatch()
				switch typ {
				case 28, 29:
					if r.PacketID != 19 || bytes.Equal(written, conn.written) {
						t.Fatal("rejected return did not resume dungeon loading")
					}
				case 30:
					if r.partyDungeonLoading {
						t.Fatal("rejected return did not resume server-start handling")
					}
				case 31:
					if !r.partyDungeonEnteredAt.IsZero() || r.partyDungeonIdentity.known {
						t.Fatal("rejected return did not resume completion handling")
					}
				}
			})
		}
	}
}

func rejectPartyReturn(t *testing.T, r *RobotVo) {
	t.Helper()
	raw := makePartyRecvPacket(7, []byte{0})
	if !r.handleReturnToSelectPacketUnsafe(robotInboundPacket{typ: 7, data: raw, size: len(raw), isAnti: true}) ||
		r.ReturnSelectPending || r.State != StateRun || !r.ReturnSelectRejected {
		t.Fatal("return rejection did not preserve the active character")
	}
}

func TestPartyReturnPausesDungeonActionsAndReliableRetries(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry%t", retry), func(t *testing.T) {
			now := time.Now()
			r, _, _ := basicAttackTestRobot(t, now)
			r.State, r.Cipher, r.Conn = StateRun, newPartyTestCipher(t), &captureSessionConn{}
			sender, receiver := comboTestTransport(t, r)
			buf := make([]byte, 4096)
			if retry {
				body := buildPartySkillStateBody(r.partySelfPeer.uniqueID, 0, nil, 0)
				if _, err := r.sendPartyReliableUnsafe(sender, r.partyPeers[0], 0, [][]byte{body}, "skill-recover", now); err != nil {
					t.Fatal(err)
				}
				receiver.SetReadDeadline(time.Now().Add(time.Second))
				if _, _, err := receiver.ReadFromUDP(buf); err != nil {
					t.Fatal(err)
				}
			}
			if !r.sendReturnToCharacterSelectUnsafe(true) {
				t.Fatal("return request failed")
			}
			r.flushPartyRuntimeUnsafe(sender, now.Add(time.Second))
			receiver.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			if _, _, err := receiver.ReadFromUDP(buf); err == nil {
				t.Fatal("pending return transmitted a dungeon action or retry")
			} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
				t.Fatal(err)
			}
			if r.partyBasicAttackStep != 0 || !r.partySkillRecoverAt.IsZero() {
				t.Fatal("pending return advanced a combat action")
			}
			rejectPartyReturn(t, r)
			r.flushPartyRuntimeUnsafe(sender, now.Add(time.Second))
			receiver.SetReadDeadline(time.Now().Add(time.Second))
			n, _, err := receiver.ReadFromUDP(buf)
			if err != nil {
				t.Fatal("rejected return did not resume transport", err)
			}
			if bodies, ok := partyDungeonFrameBodies(buf[:n]); !ok || len(bodies) == 0 {
				t.Fatalf("resumed transport sent no actor state: %x", buf[:n])
			}
		})
	}
}

func TestPartyReturnSuspendsSupervisorMaintenance(t *testing.T) {
	r := NewRobotVo(nil)
	r.State, r.ReturnSelectPending, r.partySupervisorRun = StateRun, true, true
	r.partyPeers[0] = partyIPPeer{uniqueID: 4, slot: 0, slotKnown: true}
	var maintenance time.Time
	if !r.partySupervisorStep(r.partySupervisorEpoch, r.UID, time.Now(), &maintenance) ||
		!r.partySupervisorRun || !maintenance.IsZero() {
		t.Fatal("pending return ran maintenance or stopped its supervisor")
	}
	rejectPartyReturn(t, r)
	if !r.partySupervisorStep(r.partySupervisorEpoch, r.UID, time.Now(), &maintenance) || maintenance.IsZero() {
		t.Fatal("rejected return did not resume supervisor maintenance")
	}
}

func TestPartyReturnKeepsTransportAcknowledgements(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.State, r.ReturnSelectPending = StateRun, true
	sender, receiver := comboTestTransport(t, r)
	peer := r.partyPeers[0]
	codec := partyTQOSCodec{key: 0x7e}
	request := buildPartyTQOSPacket(7, peer.slot, 0, 1, 1, codec)
	replies := r.buildPartyTQOSRepliesUnsafe(request, 1, peer)
	if len(replies) != 1 {
		t.Fatal("pending return dropped transport negotiation")
	}
	// Transport state2 retries carry no actor actions and still drain through
	// the supervisor while the character return waits for its server response.
	r.flushPartyRuntimeUnsafe(sender, now.Add(2*time.Second))
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil || !bytes.Equal(buf[:n], replies[0]) {
		t.Fatal("pending return did not preserve transport state2 retry", err)
	}
	input := buildPartyTQOSPacket(8, peer.slot, 0, 2, 1, codec)
	replies = r.buildPartyTQOSRepliesUnsafe(input, 1, peer)
	if len(replies) != 1 || !bytes.Equal(replies[0], buildPartyTQOSAck(r.partySelfPeer.slot, 8)) {
		t.Fatalf("pending return dropped reliable acknowledgement: %x", replies)
	}
}
