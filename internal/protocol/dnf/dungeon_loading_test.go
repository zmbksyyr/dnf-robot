package dnf

import (
	"bytes"
	"encoding/binary"
	"net"
	"robot/internal/protocol/dnf/crypt"
	"testing"
	"time"
)

func TestBuildFinishLoadingPayload(t *testing.T) {
	got := buildFinishLoadingPayload(0x01020304, 0xa0b0c0d0)
	want := []byte{0x04, 0x03, 0x02, 0x01, 0xd0, 0xc0, 0xb0, 0xa0}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}

func TestDungeonLoadingNotificationSendsFinishLoading(t *testing.T) {
	tests := []struct {
		name       string
		packetType uint16
	}{
		{name: "dungeon info", packetType: 28},
		{name: "start map", packetType: 29},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cipher := crypt.NewDNFCipher()
			if err := cipher.Initialize(make([]byte, 334)); err != nil {
				t.Fatalf("initialize cipher: %v", err)
			}
			robotConn, peerConn := net.Pipe()
			defer robotConn.Close()
			defer peerConn.Close()

			vo := &RobotVo{State: StateRun, Cipher: cipher, Conn: robotConn, PacketID: 7}
			packet := make([]byte, 32)
			binary.LittleEndian.PutUint16(packet[1:3], tt.packetType)
			binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
			copy(packet[15:], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17})
			if tt.packetType == 29 {
				packet = makeEncryptedPartyRecvPacket(t, cipher, 29, mustPartyHex(t, capturedRoomBody))
			}

			gotCh := make(chan []byte, 1)
			go func() {
				buf := make([]byte, 64)
				n, _ := peerConn.Read(buf)
				gotCh <- append([]byte(nil), buf[:n]...)
			}()

			vo.parsePacket(packet)
			select {
			case got := <-gotCh:
				assertFinishLoadingPacket(t, cipher, got, 7)
			case <-time.After(time.Second):
				t.Fatal("finish-loading packet was not sent")
			}
			if vo.PacketID != 8 {
				t.Fatalf("next packet id = %d, want 8", vo.PacketID)
			}
		})
	}
}

func TestShortDungeonLoadingNotificationIsIgnored(t *testing.T) {
	vo := &RobotVo{State: StateRun, Cipher: crypt.NewDNFCipher(), PacketID: 7}
	packet := make([]byte, 7)
	binary.LittleEndian.PutUint16(packet[1:3], 28)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	vo.parsePacket(packet)
	if vo.PacketID != 7 {
		t.Fatalf("short packet advanced packet id to %d", vo.PacketID)
	}
}

func TestRoomActionsWaitForServerStartAndKeepObservedPosition(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.State = StateRun
	r.Cipher = crypt.NewDNFCipher()
	if err := r.Cipher.Initialize(make([]byte, r.Cipher.KeySize())); err != nil {
		t.Fatal(err)
	}
	r.Conn = &captureSessionConn{}
	r.partySelfPeer.uniqueID = 1
	r.partyPeers[0].uniqueID = 2
	sender, receiver := comboTestTransport(t, r)
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 29, mustPartyHex(t, capturedRoomBody)))
	// This real client position preceded type30 in the second entry capture.
	// A loading client must still receive transport ACKs, but no actor replay.
	body := mustPartyHex(t, "010400bbc431877f6f7f7c7e6f7f7c7e7e7e4e2b2b3f7efe983d7efed43d")
	frame := buildPartyReliablePacket(1, 0, 0, [][]byte{body})
	replies := r.buildPartyTQOSRepliesUnsafe(frame, 1, r.partyPeers[0])
	if len(replies) != 1 || len(r.partyDungeonFollow) != 1 {
		t.Fatal("loading lost the transport ACK or queued initial position")
	}
	self := partyDungeonPlayerReference(1)
	leader := partyDungeonPlayerReference(2)
	if r.partyDungeonWorld.actors[leader].position.known != 7 {
		t.Fatal("loading discarded observed leader coordinates")
	}
	assertSilent := func() {
		t.Helper()
		r.flushPartyRuntimeUnsafe(sender, now.Add(2*time.Second))
		buf := make([]byte, 4096)
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
				t.Fatal("actor action was transmitted before server start")
			}
			// Cancellation may drain the old sequence with an empty frame.
			r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, binary.LittleEndian.Uint32(buf[1:5])), 1, r.partyPeers[0])
		}
	}
	assertSilent()
	// A rejected or truncated notification must not release queued actions.
	r.handlePartyPacketUnsafe(robotInboundPacket{typ: 30, flag: 1})
	r.handlePartyPacketUnsafe(robotInboundPacket{typ: 30, data: []byte{0}, size: 1})
	assertSilent()
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 30, make([]byte, 8)))
	r.flushPartyRuntimeUnsafe(sender, now.Add(3*time.Second))
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := partyDungeonFrameBodies(buf[:n]); !ok {
		t.Fatalf("queued movement did not resume: %x", buf[:n])
	}
	position := r.partyDungeonWorld.actors[self].position
	if position.known != 7 || position.x != 461 || position.y != 341 {
		t.Fatalf("server start lost initial actor position: %+v", position)
	}
	// The next room must close the action gate again.
	r.parsePacket(makeEncryptedPartyRecvPacket(t, r.Cipher, 29, mustPartyHex(t, capturedRoomBody)))
	r.buildPartyTQOSRepliesUnsafe(buildPartyReliablePacket(2, 0, 0, [][]byte{body}), 1, r.partyPeers[0])
	assertSilent()
}

func TestLoadingSuspendsCombatEvenWhenActorsAreKnown(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	sender, receiver := comboTestTransport(t, r)
	r.partyDungeonLoading = true
	r.flushPartyRuntimeUnsafe(sender, now)
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(buf); err == nil {
		t.Fatal("known actors allowed a combat action during loading")
	} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatal(err)
	}
	if !r.partySkillRecoverAt.IsZero() || r.partyBasicAttackStep != 0 {
		t.Fatal("loading advanced the combat sequence")
	}
	r.partyDungeonLoading = false
	r.flushPartyRuntimeUnsafe(sender, now)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	bodies, ok := partyDungeonFrameBodies(buf[:n])
	if !ok || len(bodies) != 2 || binary.LittleEndian.Uint16(bodies[1][1:3]) != 5 {
		t.Fatalf("combat did not resume after loading: %x", buf[:n])
	}
}

func TestRoomNotificationDoesNotReportUnobservedDeaths(t *testing.T) {
	// Historical diagnostic switches must not fabricate kills in the product.
	t.Setenv("DNF_LIVE_KILL_ROOM", "1")
	t.Setenv("DNF_LIVE_CLEAR_SWEEP", "1")
	vo := NewRobotVo(nil)
	vo.Cipher = crypt.NewDNFCipher()
	if err := vo.Cipher.Initialize(make([]byte, vo.Cipher.KeySize())); err != nil {
		t.Fatal(err)
	}
	conn := &captureSessionConn{}
	vo.Conn = conn
	vo.State = StateRun
	vo.PacketID = 7
	body := mustPartyHex(t, capturedRoomBody)
	vo.parsePacket(makeEncryptedPartyRecvPacket(t, vo.Cipher, 29, body))
	if vo.partyDungeonRoomAt.IsZero() {
		t.Fatal("valid room notification was not recorded")
	}
	assertFinishLoadingPacket(t, vo.Cipher, conn.written, 7)
	if vo.PacketID != 8 {
		t.Fatalf("room notification emitted extra requests: next id = %d", vo.PacketID)
	}
}

func TestMalformedRoomDoesNotConfirmLoading(t *testing.T) {
	vo := NewRobotVo(nil)
	vo.Cipher = crypt.NewDNFCipher()
	if err := vo.Cipher.Initialize(make([]byte, vo.Cipher.KeySize())); err != nil {
		t.Fatal(err)
	}
	conn := &captureSessionConn{}
	vo.Conn = conn
	vo.State = StateRun
	vo.PacketID = 7
	body := make([]byte, 16)
	body[12] = 2 // The two declared monster entries are missing.
	vo.parsePacket(makeEncryptedPartyRecvPacket(t, vo.Cipher, 29, body))
	if vo.PacketID != 7 || len(conn.written) != 0 || !vo.partyDungeonRoomAt.IsZero() {
		t.Fatal("malformed room was accepted or acknowledged")
	}
}

func TestPlainRoomNotificationConfirmsLoading(t *testing.T) {
	// Linux START_MAP was sent directly with an eight-monster table, without
	// encryption padding. The old encrypted-only path rejected its length.
	for _, cleared := range []bool{false, true} {
		vo := NewRobotVo(nil)
		vo.Cipher = crypt.NewDNFCipher()
		if err := vo.Cipher.Initialize(make([]byte, vo.Cipher.KeySize())); err != nil {
			t.Fatal(err)
		}
		conn := &captureSessionConn{}
		vo.Conn, vo.State, vo.PacketID = conn, StateRun, 7
		body := make([]byte, 149)
		body[10], body[12] = 202, 8
		if cleared {
			body = make([]byte, 12)
			body[11] = 0xff
		}
		packet := make([]byte, 15+len(body))
		binary.LittleEndian.PutUint16(packet[1:3], 29)
		binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
		copy(packet[15:], body)
		vo.parsePacket(packet)
		if vo.partyDungeonRoomAt.IsZero() || !vo.partyDungeonLoading || vo.partyRecvSource != recvBodySourcePlain {
			t.Fatalf("plain room not accepted: cleared=%t", cleared)
		}
		assertFinishLoadingPacket(t, vo.Cipher, conn.written, 7)
		if vo.PacketID != 8 {
			t.Fatalf("room emitted extra requests: next id=%d", vo.PacketID)
		}
	}
}

func TestClearedRoomConfirmsLoadingAndResetsOldWorld(t *testing.T) {
	// Captured while returning from the second Mirkwood room on 2026-11-22.
	// Treating padding byte 0x73 as a monster count left the real client black.
	body := mustPartyHex(t, "00000050eaa23200000000ff7339ce56")
	for _, anti := range []bool{false, true} {
		vo := NewRobotVo(nil)
		vo.Cipher = crypt.NewDNFCipher()
		if err := vo.Cipher.Initialize(make([]byte, vo.Cipher.KeySize())); err != nil {
			t.Fatal(err)
		}
		conn := &captureSessionConn{}
		vo.Conn, vo.State, vo.PacketID = conn, StateRun, 7
		vo.markPartyDungeonEnteredUnsafe(time.Now().Add(-time.Minute))
		vo.partyDungeonWorld.actors = map[[4]byte]partyDungeonActor{
			{0x11, 2, 1, 0}: {hp: 100, hpKnown: true},
		}
		packet := makeEncryptedPartyRecvPacket(t, vo.Cipher, 29, body)
		if anti {
			// Anti packets carry the logical body without block padding.
			packet = append(make([]byte, 15), body[:12]...)
			binary.LittleEndian.PutUint16(packet[1:3], 29)
			binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
		}
		vo.handlePartyPacketUnsafe(robotInboundPacket{data: packet, size: len(packet), typ: 29, isAnti: anti})
		assertFinishLoadingPacket(t, vo.Cipher, conn.written, 7)
		if vo.PacketID != 8 || vo.partyDungeonRoomAt.IsZero() || len(vo.partyDungeonWorld.actors) != 0 {
			t.Fatalf("anti=%t: room transition did not reset and acknowledge exactly once", anti)
		}
	}
}

func TestClearedRoomLoadingInfoRejectsIncompleteLayout(t *testing.T) {
	body := mustPartyHex(t, "00000050eaa23200000000ff7339ce56")
	if !validRoomLoadingInfo(body[:12]) || !validRoomLoadingInfo(body) {
		t.Fatal("captured cleared-room layout was rejected")
	}
	for _, offset := range []int{9, 10, 11} {
		invalid := append([]byte(nil), body...)
		invalid[offset] ^= 1
		if validRoomLoadingInfo(invalid) {
			t.Fatalf("invalid cleared-room field %d accepted", offset)
		}
	}
	for _, length := range []int{11, 13, 14, 15} {
		// A zero first padding byte must not allow the truncated short form
		// to fall through as a new-room table with no monsters.
		truncated := append([]byte(nil), body[:length]...)
		if length > 12 {
			truncated[12] = 0
		}
		if validRoomLoadingInfo(truncated) {
			t.Fatalf("truncated cleared room length %d accepted", length)
		}
	}
}

func assertFinishLoadingPacket(t *testing.T, cipher *crypt.DNFCipher, packet []byte, index uint16) {
	t.Helper()
	if len(packet) != 21 || packet[0] != 1 || binary.LittleEndian.Uint16(packet[1:3]) != 40 {
		t.Fatalf("packet header = %x", packet)
	}
	if got := binary.LittleEndian.Uint16(packet[11:13]); got != index {
		t.Fatalf("packet index = %d, want %d", got, index)
	}
	payload, err := cipher.Decrypt(40, packet[13:])
	if err != nil {
		t.Fatalf("decrypt payload: %v", err)
	}
	if !bytes.Equal(payload, make([]byte, 8)) {
		t.Fatalf("payload = %x, want zero checksums", payload)
	}
	crcInput := append(append([]byte(nil), packet[11:13]...), payload...)
	crc := cipher.CRC32(0, crcInput)
	hash := make([]byte, 4)
	binary.LittleEndian.PutUint32(hash, crc)
	hash[0] ^= hash[1] ^ hash[2] ^ hash[3] ^ 0x18
	if binary.LittleEndian.Uint32(packet[7:11]) != binary.LittleEndian.Uint32(hash) {
		t.Fatalf("packet CRC = %x, want %x", packet[7:11], hash)
	}
}
