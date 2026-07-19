package dnf

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"
)

func TestParsePartyTQOSCapturedPackets(t *testing.T) {
	tests := []struct {
		packet             string
		route, slot, state byte
	}{
		{"02000000000a000000000000b86e32b47e7d7e", 0, 0, 3},
		{"02010000000a000000000000ff4d23cb7e7d7f", 1, 0, 3},
		{"02020000000a000000000000ad0a61d17e7d7c", 2, 0, 3},
		{"02000000000a0001000000009110de907f7d7f", 1, 1, 3},
		{"01070000000a0000000000005fcfd1967e7c7f", 1, 0, 2},
	}
	for _, tt := range tests {
		got, ok := parsePartyTQOSPacket(mustPartyHex(t, tt.packet), tt.route)
		if !ok || got.senderSlot != tt.slot || got.state != tt.state || got.route != tt.route || got.codec != (partyTQOSCodec{key: 0x7e}) {
			t.Fatalf("decoded packet = %+v ok=%v", got, ok)
		}
	}
}

func TestPartyTQOSReliableInnerRecord(t *testing.T) {
	packet := buildPartyTQOSPacket(7, 1, 0, 2, 1, partyTQOSCodec{key: 0x7e})
	packet = append(packet, 0x0b, 0x00)
	packet = append(packet, make([]byte, 11)...)
	binary.LittleEndian.PutUint16(packet[5:7], uint16(len(packet)-9))

	got, ok := parsePartyTQOSPacket(packet, 1)
	if !ok || got.typ != 1 || got.sequence != 7 || got.senderSlot != 1 || got.state != 2 {
		t.Fatalf("packet = %+v ok=%v", got, ok)
	}
	want := "01000000000c0001000a0000000031922ccd7f7c7f"
	if got := hex.EncodeToString(buildPartyTQOSPacket(0, 1, 0, 2, 1, partyTQOSCodec{key: 0x7e})); got != want {
		t.Fatalf("reliable state2 = %s, want %s", got, want)
	}
}

func TestRobotPartyTQOSStateMachine(t *testing.T) {
	vo := &RobotVo{UID: 17000014}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x2749, accID: 18000000, slot: 0, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	inputs := []string{
		"02000000000a000000000000ff4d23cb7e7d7f",
		"02050000000a00000000000085e845b67e7e7f",
		"02060000000a000000000000256ab7eb7e7f7f",
	}
	wants := []string{
		"02000000000a000100000000ebb5b8ed7f7e7f",
		"02010000000a0001000000004b374ab07f7f7f",
		"01000000000c0001000a0000000031922ccd7f7c7f",
	}
	for i := range inputs {
		got := vo.buildPartyUDPAcks(mustPartyHex(t, inputs[i]), remote)
		if len(got) != 1 || hex.EncodeToString(got[0]) != wants[i] {
			t.Fatalf("step %d replies=%x, want %s", i, got, wants[i])
		}
	}
	ack := vo.buildPartyUDPAcks(mustPartyHex(t, "01070000000a0000000000005fcfd1967e7c7f"), remote)
	if len(ack) != 1 || hex.EncodeToString(ack[0]) != "0001080000000000" {
		t.Fatalf("reliable ack = %x", ack)
	}
	if vo.partyTQOSSeq[0][1] != 2 || vo.partyTQOSReliableSeq[0][1] != 1 {
		t.Fatalf("sequences = %d/%d, want 2/1", vo.partyTQOSSeq[0][1], vo.partyTQOSReliableSeq[0][1])
	}
}

func TestRobotPartyTQOSSequencesAreIsolatedByPeerAndRoute(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 2, slotKnown: true}
	leader := partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true}
	robotPeer := partyIPPeer{uniqueID: 2, accID: 17000002, slot: 1, slotKnown: true}
	vo.partyPeers[0] = leader
	vo.partyPeers[1] = robotPeer

	leaderRoute1 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(10, leader.slot, 0, 3, 1, codec), 1, leader)
	robotRoute1 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(20, robotPeer.slot, 0, 3, 1, codec), 1, robotPeer)
	leaderRoute2 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(30, leader.slot, 0, 3, 2, codec), 2, leader)
	for name, replies := range map[string][][]byte{
		"leader route1": leaderRoute1,
		"robot route1":  robotRoute1,
		"leader route2": leaderRoute2,
	} {
		if len(replies) != 1 {
			t.Fatalf("%s replies = %x", name, replies)
		}
		if sequence := binary.LittleEndian.Uint32(replies[0][1:5]); sequence != 0 {
			t.Fatalf("%s sequence = %d, want 0", name, sequence)
		}
	}

	leaderRoute1 = vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(11, leader.slot, 0, 3, 1, codec), 1, leader)
	if len(leaderRoute1) != 1 || binary.LittleEndian.Uint32(leaderRoute1[0][1:5]) != 1 {
		t.Fatalf("leader route1 second replies = %x", leaderRoute1)
	}
	if vo.partyTQOSSeq[0][1] != 2 || vo.partyTQOSSeq[1][1] != 1 || vo.partyTQOSSeq[0][2] != 1 {
		t.Fatalf("isolated sequences = %+v", vo.partyTQOSSeq)
	}
	if !vo.partyRobotPeerReady[1] {
		t.Fatal("robot peer TQOS was not marked ready")
	}
}

func TestRobotPartyReliableTransportAckCompositeFrames(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	buildFrame := func(seq uint32, bodySize int) []byte {
		frame := make([]byte, 9+bodySize)
		frame[0] = 1
		binary.LittleEndian.PutUint32(frame[1:5], seq)
		binary.LittleEndian.PutUint16(frame[5:7], uint16(bodySize))
		return frame
	}
	payload := append(buildFrame(0x1b2, 39), buildFrame(0x1b3, 52)...)
	got := vo.buildPartyUDPAcks(payload, remote)
	if len(got) != 2 || hex.EncodeToString(got[0]) != "0001b30100000000" || hex.EncodeToString(got[1]) != "0001b40100000000" {
		t.Fatalf("composite ACKs = %x", got)
	}
}

func TestRobotPartyTQOSSelectsPeerBySenderSlot(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 2, slotKnown: true}
	endpoint := net.IPv4(192, 168, 200, 1)
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true, outerIP: endpoint, port: 5063}
	vo.partyPeers[1] = partyIPPeer{uniqueID: 2, slot: 1, slotKnown: true, outerIP: endpoint, port: 5063}
	remote := &net.UDPAddr{IP: endpoint, Port: 5063}
	replies := vo.buildPartyUDPAcks(mustPartyHex(t, "02000000000a0001000000009110de907f7d7f"), remote)
	if len(replies) != 1 {
		t.Fatalf("replies = %x", replies)
	}
	got, ok := parsePartyTQOSPacket(replies[0], 1)
	if !ok || got.senderSlot != 2 || got.state != 0 {
		t.Fatalf("reply = %+v ok=%v", got, ok)
	}
}

func TestRobotPartyTQOSUsesCachedCodec(t *testing.T) {
	codec := partyTQOSCodec{key: 0xa5, rotate: 3}
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 0, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 1, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	if got := vo.buildPartyUDPAcks(buildPartyTQOSPacket(7, 1, 0, 3, 1, codec), remote); len(got) != 1 {
		t.Fatalf("state3 replies = %x", got)
	}
	replies := vo.buildPartyUDPAcks(buildPartyTQOSPacket(8, 1, 0, 1, 1, codec), remote)
	if len(replies) != 1 {
		t.Fatalf("state1 replies = %x", replies)
	}
	got, ok := parsePartyTQOSPacketWithCodec(replies[0], 1, &codec)
	if !ok || got.typ != 1 || got.state != 2 || got.codec != codec {
		t.Fatalf("state2 = %+v ok=%v", got, ok)
	}
}
