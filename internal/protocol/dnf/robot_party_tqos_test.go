package dnf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
	"time"
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

func TestRobotPartyTQOSRetransmitsPendingReliableReply(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}
	request := buildPartyTQOSPacket(7, peer.slot, 0, 1, 1, codec)

	first := vo.buildPartyTQOSRepliesUnsafe(request, 1, peer)
	second := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(8, peer.slot, 0, 1, 1, codec), 1, peer)
	if len(first) != 1 || len(second) != 1 || !bytes.Equal(first[0], second[0]) {
		t.Fatalf("reliable retry changed reply: first=%x second=%x", first, second)
	}
	if vo.partyTQOSReliableSeq[peer.slot][1] != 1 {
		t.Fatalf("reliable sequence advanced on retry: %d", vo.partyTQOSReliableSeq[peer.slot][1])
	}
	if got := vo.partyTQOSReplies[peer.slot][1].latestRequestSequence; got != 8 {
		t.Fatalf("latest request sequence = %d, want 8", got)
	}

	sequence := binary.LittleEndian.Uint32(first[0][1:5])
	if replies := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, sequence), 1, peer); len(replies) != 0 {
		t.Fatalf("ACK generated replies: %x", replies)
	}
	duplicate := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(8, peer.slot, 0, 1, 1, codec), 1, peer)
	if len(duplicate) != 1 || !bytes.Equal(duplicate[0], first[0]) || vo.partyTQOSReliableSeq[peer.slot][1] != 1 {
		t.Fatalf("post-ACK duplicate changed reply=%x first=%x next=%d", duplicate, first, vo.partyTQOSReliableSeq[peer.slot][1])
	}
	third := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(9, peer.slot, 0, 1, 1, codec), 1, peer)
	if len(third) != 1 || bytes.Equal(third[0], first[0]) || vo.partyTQOSReliableSeq[peer.slot][1] != 2 {
		t.Fatalf("new handshake reused reply=%x first=%x next=%d", third, first, vo.partyTQOSReliableSeq[peer.slot][1])
	}
}

func TestMarkPartyRobotPeerReadyClearsPendingReplies(t *testing.T) {
	vo := &RobotVo{}
	vo.partyTQOSReplies[1][1] = partyTQOSReliableReply{
		packet:    []byte{1, 2, 3, 4, 5},
		nextRetry: time.Now(),
	}

	vo.markPartyRobotPeerReadyUnsafe(partyIPPeer{accID: 17000001, slot: 1, slotKnown: true}, "test")
	if len(vo.partyTQOSReplies[1][1].packet) != 0 || !vo.partyTQOSReplies[1][1].nextRetry.IsZero() {
		t.Fatalf("pending reply was retained after ready: %+v", vo.partyTQOSReplies[1][1])
	}
}

func TestRobotPartyTQOSReliableReplyStartsNewEpochForCodecOrFlags(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}
	codecA := partyTQOSCodec{key: 0x7e}
	codecB := partyTQOSCodec{key: 0xa5, rotate: 3}

	first := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(7, peer.slot, 0, 1, 1, codecA), 1, peer)
	second := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(8, peer.slot, 1, 1, 1, codecA), 1, peer)
	third := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(9, peer.slot, 1, 1, 1, codecB), 1, peer)
	if len(first) != 1 || len(second) != 1 || len(third) != 1 || bytes.Equal(first[0], second[0]) || bytes.Equal(second[0], third[0]) {
		t.Fatalf("epochs first=%x second=%x third=%x", first, second, third)
	}
	if vo.partyTQOSReliableSeq[peer.slot][1] != 3 {
		t.Fatalf("reliable sequence = %d, want 3", vo.partyTQOSReliableSeq[peer.slot][1])
	}
	got, ok := parsePartyTQOSPacketWithCodec(third[0], 1, &codecB)
	if !ok || got.flags != 1 || got.state != 2 || got.codec != codecB {
		t.Fatalf("latest reply = %+v ok=%t", got, ok)
	}
}

func TestRobotPartyTQOSExhaustedReplyRecoversOnNewRequest(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}
	first := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(7, peer.slot, 0, 1, 1, codec), 1, peer)
	pending := &vo.partyTQOSReplies[peer.slot][1]
	pending.exhausted = true
	pending.retries = partyTQOSMaxRetries
	pending.nextRetry = time.Time{}

	second := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(8, peer.slot, 0, 1, 1, codec), 1, peer)
	if len(first) != 1 || len(second) != 1 || bytes.Equal(first[0], second[0]) {
		t.Fatalf("exhausted epoch was not replaced: first=%x second=%x", first, second)
	}
	if pending.exhausted || pending.retries != 0 || pending.nextRetry.IsZero() {
		t.Fatalf("new pending state = %+v", *pending)
	}
}

func TestPartyTQOSSequenceAfterHandlesWraparound(t *testing.T) {
	if !partyTQOSSequenceAfter(0, ^uint32(0)) {
		t.Fatal("wrapped sequence was not newer")
	}
	if partyTQOSSequenceAfter(^uint32(0), 0) {
		t.Fatal("old wrapped sequence was newer")
	}
}

func TestRobotPartyTQOSPeriodicallyRetransmitsOriginalState2UntilACK(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{UID: 17000001}
	vo.partySelfPeer = partyIPPeer{uniqueID: 2, accID: 17000001, slot: 1, slotKnown: true}
	remote := receiver.LocalAddr().(*net.UDPAddr)
	peer := partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	vo.partyPeers[0] = peer

	replies := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(7, peer.slot, 0, 1, 1, codec), 1, peer)
	if len(replies) != 1 {
		t.Fatalf("state1 replies = %x", replies)
	}
	original := append([]byte(nil), replies[0]...)
	now := time.Unix(400, 0)
	vo.partyTQOSReplies[peer.slot][1].nextRetry = now
	vo.flushPartyTQOSRepliesUnsafe(sender, now)

	buf := make([]byte, 128)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], original) {
		t.Fatalf("periodic retry = %x, want original %x", buf[:n], original)
	}
	if vo.partyTQOSReliableSeq[peer.slot][1] != 1 || vo.partyTQOSReplies[peer.slot][1].retries != 1 {
		t.Fatalf("retry state sequence=%d retries=%d", vo.partyTQOSReliableSeq[peer.slot][1], vo.partyTQOSReplies[peer.slot][1].retries)
	}

	sequence := binary.LittleEndian.Uint32(original[1:5])
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, sequence), 1, peer)
	if !vo.partyTQOSReplies[peer.slot][1].acknowledged {
		t.Fatal("matching ACK did not stop the pending state2")
	}
	vo.flushPartyTQOSRepliesUnsafe(sender, now.Add(2*partyTQOSRetryInterval))
	_ = receiver.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(buf); err == nil {
		t.Fatal("acknowledged state2 was retransmitted")
	}
}

func TestRobotPartyTQOSPeriodicState2RetryIsBounded(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	vo := &RobotVo{UID: 17000001}
	vo.partySelfPeer = partyIPPeer{uniqueID: 2, accID: 17000001, slot: 1, slotKnown: true}
	remote := receiver.LocalAddr().(*net.UDPAddr)
	peer := partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	vo.partyPeers[0] = peer
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(7, peer.slot, 0, 1, 1, partyTQOSCodec{key: 0x7e}), 1, peer)

	buf := make([]byte, 128)
	base := time.Unix(500, 0)
	for retry := 0; retry < partyTQOSMaxRetries; retry++ {
		vo.partyTQOSReplies[peer.slot][1].nextRetry = base.Add(time.Duration(retry) * partyTQOSRetryInterval)
		vo.flushPartyTQOSRepliesUnsafe(sender, base.Add(time.Duration(retry)*partyTQOSRetryInterval))
		_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err := receiver.ReadFromUDP(buf); err != nil {
			t.Fatalf("retry %d: %v", retry+1, err)
		}
	}
	pending := &vo.partyTQOSReplies[peer.slot][1]
	vo.flushPartyTQOSRepliesUnsafe(sender, pending.nextRetry)
	if !pending.exhausted || pending.retries != partyTQOSMaxRetries || !pending.nextRetry.IsZero() {
		t.Fatalf("bounded retry state = %+v", *pending)
	}
	_ = receiver.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(buf); err == nil {
		t.Fatal("exhausted state2 was retransmitted")
	}
}

func TestRobotPartyReliableRetransmitOnlyQueuesFollowOnce(t *testing.T) {
	vo := &RobotVo{}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0xee9f, slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 0xf4a1, slot: 0, slotKnown: true}
	body := mustPartyHex(t, "015100a12a3fca010700677716ec00010700677716ec11026003a1f4f10200000d000000ffffffffffffffff0000000000000000")
	frame := buildPartyReliablePacket(7, peer.slot, 0, [][]byte{body})

	first := vo.buildPartyTQOSRepliesUnsafe(frame, 1, peer)
	second := vo.buildPartyTQOSRepliesUnsafe(frame, 1, peer)
	if len(first) != 1 || len(second) != 1 || len(vo.partyDungeonFollow) != 1 {
		t.Fatalf("first=%x second=%x queued=%d", first, second, len(vo.partyDungeonFollow))
	}
	if !bytes.Equal(first[0], second[0]) {
		t.Fatalf("retransmit ACK changed: first=%x second=%x", first, second)
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
	if vo.partyRobotPeerReady[1] {
		t.Fatal("robot peer was marked ready before state2 or ACK")
	}
}

func TestRobotPartyTQOSState3StartsNewReceiveEpoch(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	for _, route := range []byte{1, 2} {
		t.Run(fmt.Sprintf("route%d", route), func(t *testing.T) {
			vo := &RobotVo{}
			vo.partySelfPeer = partyIPPeer{slot: 1, slotKnown: true}
			peer := partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}

			vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(100, peer.slot, 0, 2, route, codec), route, peer)
			vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(101, peer.slot, 0, 1, route, codec), route, peer)
			vo.partyTQOSSeq[peer.slot][route] = 9
			vo.partyTQOSReliableSeq[peer.slot][route] = 11
			vo.partyRobotPeerReady[peer.slot] = true
			if window := vo.partyTQOSReceived[peer.slot][route]; !window.initialized || window.latest != 100 {
				t.Fatalf("advanced receive window = %+v", window)
			}
			if len(vo.partyTQOSReplies[peer.slot][route].packet) == 0 {
				t.Fatal("state2 reply was not pending before reconnect")
			}

			state3 := buildPartyTQOSPacket(1, peer.slot, 0, 3, route, codec)
			vo.buildPartyTQOSRepliesUnsafe(state3, route, peer)
			if window := vo.partyTQOSReceived[peer.slot][route]; window.initialized {
				t.Fatalf("state3 retained old receive window = %+v", window)
			}
			if pending := vo.partyTQOSReplies[peer.slot][route]; len(pending.packet) != 0 {
				t.Fatalf("state3 retained old state2 reply = %+v", pending)
			}
			if vo.partyTQOSSeq[peer.slot][route] != 1 || vo.partyTQOSReliableSeq[peer.slot][route] != 0 || vo.partyRobotPeerReady[peer.slot] {
				t.Fatalf("state3 did not reset outbound epoch: seq=%d reliable=%d ready=%t", vo.partyTQOSSeq[peer.slot][route], vo.partyTQOSReliableSeq[peer.slot][route], vo.partyRobotPeerReady[peer.slot])
			}

			state1 := buildPartyTQOSPacket(2, peer.slot, 0, 1, route, codec)
			replies := vo.buildPartyTQOSRepliesUnsafe(state1, route, peer)
			if len(replies) != 1 || binary.LittleEndian.Uint32(replies[0][1:5]) != 0 || vo.partyTQOSReliableSeq[peer.slot][route] != 1 {
				t.Fatalf("new epoch state2 did not restart at zero: replies=%x next=%d", replies, vo.partyTQOSReliableSeq[peer.slot][route])
			}
			pending := append([]byte(nil), vo.partyTQOSReplies[peer.slot][route].packet...)
			vo.buildPartyTQOSRepliesUnsafe(state3, route, peer)
			if !bytes.Equal(vo.partyTQOSReplies[peer.slot][route].packet, pending) || vo.partyTQOSReliableSeq[peer.slot][route] != 1 {
				t.Fatalf("duplicate state3 reset active epoch: pending=%x next=%d", vo.partyTQOSReplies[peer.slot][route].packet, vo.partyTQOSReliableSeq[peer.slot][route])
			}

			vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(3, peer.slot, 0, 2, route, codec), route, peer)
			if window := vo.partyTQOSReceived[peer.slot][route]; !window.initialized || window.latest != 3 {
				t.Fatalf("low reconnect sequence was rejected: %+v", window)
			}
		})
	}
}

func TestRobotPartyTQOSReadyRequiresState2OrReplyACK(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{UID: 17000001}
	vo.partySelfPeer = partyIPPeer{accID: 17000001, slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 1, accID: 17000002, slot: 0, slotKnown: true}

	state3 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(1, peer.slot, 0, 3, 1, codec), 1, peer)
	if len(state3) != 1 || vo.partyRobotPeerReady[peer.slot] {
		t.Fatalf("state3 replies=%x ready=%t", state3, vo.partyRobotPeerReady[peer.slot])
	}
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(2, peer.slot, 0, 0, 1, codec), 1, peer)
	state1 := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(3, peer.slot, 0, 1, 1, codec), 1, peer)
	if len(state1) != 1 || vo.partyRobotPeerReady[peer.slot] {
		t.Fatalf("state1 replies=%x ready=%t", state1, vo.partyRobotPeerReady[peer.slot])
	}
	sequence := binary.LittleEndian.Uint32(state1[0][1:5])
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, sequence), 1, peer)
	if !vo.partyRobotPeerReady[peer.slot] {
		t.Fatal("state2 ACK did not mark robot peer ready")
	}

	vo.resetPartyTQOSPeerUnsafe(peer.slot)
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(4, peer.slot, 0, 2, 1, codec), 1, peer)
	if !vo.partyRobotPeerReady[peer.slot] {
		t.Fatal("received state2 did not mark robot peer ready")
	}

	vo.resetPartyTQOSPeerUnsafe(peer.slot)
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(5, peer.slot, 0, 2, 2, codec), 2, peer)
	if !vo.partyRobotPeerReady[peer.slot] {
		t.Fatal("relay state2 did not mark robot peer ready")
	}
}

func TestRobotPartyTQOSRelayACKDoesNotConfirmNewEpoch(t *testing.T) {
	codec := partyTQOSCodec{key: 0x7e}
	vo := &RobotVo{UID: 17000001}
	vo.partySelfPeer = partyIPPeer{accID: 17000001, slot: 1, slotKnown: true}
	peer := partyIPPeer{uniqueID: 1, accID: 17000002, slot: 0, slotKnown: true}

	first := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(7, peer.slot, 0, 1, 2, codec), 2, peer)
	if len(first) != 1 {
		t.Fatalf("first relay state2 = %x", first)
	}
	firstSequence := binary.LittleEndian.Uint32(first[0][1:5])
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, firstSequence), 2, peer)
	if !vo.partyTQOSReplies[peer.slot][2].acknowledged || !vo.partyRobotPeerReady[peer.slot] {
		t.Fatal("relay ACK did not complete the first epoch")
	}

	second := vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSPacket(8, peer.slot, 0, 1, 2, codec), 2, peer)
	if len(second) != 1 || bytes.Equal(first[0], second[0]) {
		t.Fatalf("second relay epoch = %x first=%x", second, first)
	}
	vo.partyRobotPeerReady[peer.slot] = false
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, firstSequence), 2, peer)
	if vo.partyTQOSReplies[peer.slot][2].acknowledged || vo.partyRobotPeerReady[peer.slot] {
		t.Fatal("old relay ACK confirmed the new epoch")
	}
	secondSequence := binary.LittleEndian.Uint32(second[0][1:5])
	vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(peer.slot, secondSequence), 2, peer)
	if !vo.partyTQOSReplies[peer.slot][2].acknowledged || !vo.partyRobotPeerReady[peer.slot] {
		t.Fatal("current relay ACK did not complete the new epoch")
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
