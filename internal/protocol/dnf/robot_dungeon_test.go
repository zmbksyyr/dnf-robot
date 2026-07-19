package dnf

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"robot/internal/shared"
)

func TestRobotRewritesCapturedDungeonEnvelopeIdentity(t *testing.T) {
	tests := []string{
		"015100a12a3fca010700677716ec00010700677716ec11026003a1f4f10200000d000000ffffffffffffffff0000000000000000",
		"0151006635ba98010c0060cd648600010c0060cd64861101a1f4484e020000000000ffffffff",
	}
	for _, captured := range tests {
		body := mustPartyHex(t, captured)
		follow, command, ok := rewritePartyDungeonBody(body, 0xf4a1, 0xee9f)
		if !ok || command != partyDungeonEnvelopeCommand {
			t.Fatalf("rewrite envelope command=%x ok=%t body=%x", command, ok, follow)
		}
		if bytes.Contains(follow[partyDungeonEnvelopePayloadOffset:], []byte{0xa1, 0xf4}) || !bytes.Contains(follow[partyDungeonEnvelopePayloadOffset:], []byte{0x9f, 0xee}) {
			t.Fatalf("envelope identity was not rewritten: %x", follow)
		}
		innerChecksum := partyPayloadChecksum(follow[partyDungeonEnvelopePayloadOffset:])
		for _, offset := range partyDungeonEnvelopeChecksumOffsets {
			if !bytes.Equal(follow[offset:offset+4], innerChecksum[:]) {
				t.Fatalf("inner checksum at %d = %x, want %x", offset, follow[offset:offset+4], innerChecksum)
			}
		}
		checksum := partyPayloadChecksum(follow[7:])
		if !bytes.Equal(follow[3:7], checksum[:]) {
			t.Fatalf("outer checksum = %x, want %x", follow[3:7], checksum)
		}
	}
}

func TestRobotRewritesCapturedDungeonStateIdentity(t *testing.T) {
	body := mustPartyHex(t, "0104007db257987fff7edf8a6f7fdf8a7c7e7e143b1f34876a3a7efefbbd")
	follow, command, ok := rewritePartyDungeonBody(body, 0xf4a1, 0xee9f)
	if !ok || command != partyDungeonStateCommand {
		t.Fatalf("rewrite state command=%x ok=%t body=%x", command, ok, follow)
	}
	plain := append([]byte(nil), follow[7:]...)
	for i := range plain {
		plain[i] ^= 0x7e
	}
	if bytes.Contains(plain, []byte{0xa1, 0xf4}) || bytes.Count(plain, []byte{0x9f, 0xee}) != 2 {
		t.Fatalf("state identity was not rewritten: %x", plain)
	}
	checksum := partyPayloadChecksum(plain)
	if !bytes.Equal(follow[3:7], checksum[:]) {
		t.Fatalf("state checksum = %x, want %x", follow[3:7], checksum)
	}
}

func TestRobotBuildsDungeonFollowWithOwnSlotAndPeerSequence(t *testing.T) {
	body := mustPartyHex(t, "0104007db257987fff7edf8a6f7fdf8a7c7e7e143b1f34876a3a7efefbbd")
	frame := buildPartyUnreliablePacket(91, 0, 7, body)
	vo := &RobotVo{UID: 17000002, partySelfPeer: partyIPPeer{slot: 2, slotKnown: true, uniqueID: 0xee9f}}
	peer := partyIPPeer{slot: 0, slotKnown: true, uniqueID: 0xf4a1}

	follow := flushQueuedDungeonFollow(t, vo, frame, peer)
	if follow[0] != 0x02 || binary.LittleEndian.Uint32(follow[1:5]) != 0 || follow[7] != 2 || follow[8] != 7 {
		t.Fatalf("follow frame = %x", follow)
	}
	if vo.partyTQOSSeq[0][1] != 1 {
		t.Fatalf("leader route sequence = %d, want 1", vo.partyTQOSSeq[0][1])
	}
	plain := append([]byte(nil), follow[16:]...)
	for i := range plain {
		plain[i] ^= 0x7e
	}
	if bytes.Count(plain, []byte{0x9f, 0xee}) != 2 {
		t.Fatalf("follow state identity = %x", plain)
	}
}

func TestRobotDoesNotRewriteMultiEntityDungeonState(t *testing.T) {
	body := mustPartyHex(t, "0104006e4210937cff7edf8a6f7fdf8a7c7e7e143b1f4e30c43d7e7efabd6f7c1e7d6f7fdf8a7c7e7e14271cbcedb43d")
	if follow, _, ok := rewritePartyDungeonBody(body, 0xf4a1, 0xee9f); ok || follow != nil {
		t.Fatalf("multi-entity state was rewritten: %x", follow)
	}
}

func TestRobotDoesNotFollowPartySkillState(t *testing.T) {
	now := time.Now()
	peer := partyIPPeer{uniqueID: 0x1111, slot: 0, slotKnown: true}
	vo := &RobotVo{UID: 17000001, partySelfPeer: partyIPPeer{uniqueID: 0x2222, slot: 1, slotKnown: true}}
	body := buildPartySkillStateBody(peer.uniqueID, 22, []byte{3, 0, 0}, 7)
	if vo.queuePartyDungeonFollowUnsafe(buildPartyUnreliablePacket(1, peer.slot, 0, body), peer, now) {
		t.Fatal("standalone skill state entered the follow queue")
	}
	if vo.queuePartyDungeonFollowUnsafe(buildPartyReliablePacket(1, peer.slot, 0, [][]byte{body}), peer, now) {
		t.Fatal("reliable skill state entered the follow queue")
	}
}

func TestRobotBuildsReliableDungeonEnvelope(t *testing.T) {
	record := mustPartyHex(t, "0151006635ba98010c0060cd648600010c0060cd64861101a1f4484e020000000000ffffffff")
	frame := buildPartyReliablePacket(42, 0, 3, [][]byte{record})
	vo := &RobotVo{UID: 17000003, partySelfPeer: partyIPPeer{slot: 1, slotKnown: true, uniqueID: 0xee9f}}
	peer := partyIPPeer{slot: 0, slotKnown: true, uniqueID: 0xf4a1}

	follow := flushQueuedDungeonFollow(t, vo, frame, peer)
	if follow[0] != 0x01 || binary.LittleEndian.Uint32(follow[1:5]) != 0 || follow[7] != 1 || follow[8] != 3 {
		t.Fatalf("reliable follow frame = %x", follow)
	}
	if vo.partyTQOSReliableSeq[0][1] != 1 || binary.LittleEndian.Uint16(follow[9:11]) != uint16(len(record)) {
		t.Fatalf("reliable state sequence=%d frame=%x", vo.partyTQOSReliableSeq[0][1], follow)
	}
	if bytes.Contains(follow[11+partyDungeonEnvelopePayloadOffset:], []byte{0xa1, 0xf4}) || !bytes.Contains(follow[11+partyDungeonEnvelopePayloadOffset:], []byte{0x9f, 0xee}) {
		t.Fatalf("reliable envelope identity was not rewritten: %x", follow)
	}
}

func TestRobotPeerResetDropsOnlyItsDelayedDungeonFrames(t *testing.T) {
	vo := &RobotVo{partyDungeonFollow: []partyDungeonFollowPending{{peerSlot: 0}, {peerSlot: 2}, {peerSlot: 0}}}
	vo.resetPartyTQOSPeerUnsafe(0)
	if len(vo.partyDungeonFollow) != 1 || vo.partyDungeonFollow[0].peerSlot != 2 {
		t.Fatalf("delayed frames after reset = %+v", vo.partyDungeonFollow)
	}
}

func TestBuildPartySkillStateBody(t *testing.T) {
	body := buildPartySkillStateBody(0x2f3e, 0x16, nil, 0xe708)
	if len(body) != partySkillStateBodyBaseSize || body[0] != 2 || binary.LittleEndian.Uint16(body[1:3]) != partyDungeonEnvelopeCommand {
		t.Fatalf("skill body header = %x", body)
	}
	payload := body[partyDungeonEnvelopePayloadOffset:]
	wantPayload := mustPartyHex(t, "11013e2f16000008e7")
	if !bytes.Equal(payload, wantPayload) {
		t.Fatalf("skill payload = %x, want %x", payload, wantPayload)
	}
	inner := partyPayloadChecksum(payload)
	for _, offset := range partyDungeonEnvelopeChecksumOffsets {
		if !bytes.Equal(body[offset:offset+4], inner[:]) {
			t.Fatalf("inner checksum at %d = %x, want %x", offset, body[offset:offset+4], inner)
		}
	}
	outer := partyPayloadChecksum(body[7:])
	if body[3] != outer[0] {
		t.Fatalf("outer checksum byte = %x, want %x", body[3], outer[0])
	}
	if bytes.Equal(body[4:7], []byte{0, 0, 0}) {
		t.Fatalf("outer context bytes were zero: %x", body)
	}
}

func TestBuildPartySkillStateBodyMatchesCapturedShiningCut(t *testing.T) {
	body := buildPartySkillStateBody(0x2f3e, 0x16, []byte{0x03, 0x00, 0x00}, 0xe708)
	want := mustPartyHex(t, "0251001b505afc0205000fc901df000205000fc901df11013e2f16030003000008e7")
	if !bytes.Equal(body[:4], want[:4]) || !bytes.Equal(body[7:], want[7:]) {
		t.Fatalf("shining cut body = %x, want stable bytes from %x", body, want)
	}
	if bytes.Equal(body[4:7], []byte{0, 0, 0}) {
		t.Fatalf("shining cut outer context bytes were zero: %x", body)
	}
}

func TestPartySkillCandidatesRequireWhitelistAndPVF(t *testing.T) {
	whitelist := []shared.PartySkillState{
		{Job: 6, SkillIndex: 3, State: 22, Level: 5, Name: "ok", StateData: []byte{3, 0, 0}, Risk: 1, ScriptPath: "ok.nut"},
		{Job: 6, SkillIndex: 4, State: 23, Level: 10, Name: "unlearned", StateData: []byte{0, 0, 0}, Risk: 1},
		{Job: 6, SkillIndex: 5, State: 24, Level: 10, Name: "missing_pvf", StateData: []byte{0, 0, 0}, Risk: 1},
		{Job: 6, SkillIndex: 7, State: 26, Level: 10, Name: "wrong_path", StateData: []byte{0, 0, 0}, Risk: 1, ScriptPath: "expected/skill.nut"},
		{Job: 2, SkillIndex: 6, State: 25, Level: 10, StateData: []byte{0, 0, 0}, Risk: 1},
	}
	pvfStates := []shared.SkillState{
		{Job: 6, SkillIndex: 3, State: 22, ScriptPath: "OK.NUT"},
		{Job: 6, SkillIndex: 4, State: 23},
		{Job: 6, SkillIndex: 7, State: 26, ScriptPath: "other/skill.nut"},
		{Job: 2, SkillIndex: 6, State: 25},
	}
	got, stats := partySkillCandidatesFromCatalog(6, whitelist, pvfStates)
	if len(got) != 2 || got[0].skillIndex != 3 || got[0].state != 22 || !bytes.Equal(got[0].stateData, []byte{3, 0, 0}) {
		t.Fatalf("candidates = %+v", got)
	}
	if got[1].skillIndex != 4 || got[1].state != 23 {
		t.Fatalf("second whitelist candidate = %+v", got[1])
	}
	if stats.PVFMatched != 2 || stats.SkippedMissingPVF != 1 || stats.SkippedPathMismatch != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestRobotSendsBoundedDungeonSkillState(t *testing.T) {
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
	remote := receiver.LocalAddr().(*net.UDPAddr)
	now := time.Unix(200, 0)
	vo := &RobotVo{
		UID:                  17000001,
		partySelfPeer:        partyIPPeer{uniqueID: 0x2f3e, slot: 2, slotKnown: true},
		partyDungeonLastAt:   now,
		partyDungeonFlags:    5,
		partySkillNextAt:     now.Add(-time.Millisecond),
		partySkillLoaded:     true,
		partySkillJob:        8,
		partySkillCandidates: []partySkillCandidate{{skillIndex: 3, state: 22, stateData: []byte{0x03, 0x00, 0x00}}},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
	vo.flushPartyDungeonSkillUnsafe(sender, now)

	buf := make([]byte, 4096)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	packet := buf[:n]
	if packet[0] != 1 || binary.LittleEndian.Uint32(packet[1:5]) != 0 || packet[7] != 2 || packet[8] != 5 {
		t.Fatalf("skill transport = %x", packet)
	}
	if vo.partyTQOSReliableSeq[0][1] != 1 || binary.LittleEndian.Uint16(packet[9:11]) != partySkillStateBodyBaseSize+3 {
		t.Fatalf("skill sequence=%d packet=%x", vo.partyTQOSReliableSeq[0][1], packet)
	}
	body := packet[11:]
	if binary.LittleEndian.Uint16(body[partyDungeonEnvelopePayloadOffset+2:]) != 0x2f3e || body[partyDungeonEnvelopePayloadOffset+4] != 22 || !bytes.Equal(body[partyDungeonEnvelopePayloadOffset+7:partyDungeonEnvelopePayloadOffset+10], []byte{0x03, 0x00, 0x00}) {
		t.Fatalf("skill state identity = %x", body)
	}
	if vo.partySkillRecoverAt.Sub(now) != partySkillRecoverDelay {
		t.Fatalf("recover delay = %s", vo.partySkillRecoverAt.Sub(now))
	}
	vo.flushPartyDungeonSkillUnsafe(sender, now.Add(partySkillRecoverDelay))
	n, _, err = receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	packet = buf[:n]
	if vo.partyTQOSReliableSeq[0][1] != 2 || binary.LittleEndian.Uint16(packet[9:11]) != partySkillStateBodyBaseSize {
		t.Fatalf("recover sequence=%d packet=%x", vo.partyTQOSReliableSeq[0][1], packet)
	}
	body = packet[11:]
	if binary.LittleEndian.Uint16(body[partyDungeonEnvelopePayloadOffset+2:]) != 0x2f3e || body[partyDungeonEnvelopePayloadOffset+4] != 0 || binary.LittleEndian.Uint16(body[partyDungeonEnvelopePayloadOffset+5:]) != 0 {
		t.Fatalf("recover state identity = %x", body)
	}
	if !vo.partySkillRecoverAt.IsZero() {
		t.Fatalf("recover timer survived send: %s", vo.partySkillRecoverAt)
	}
	if vo.partySkillNextAt.Sub(now) < 4*time.Second || vo.partySkillNextAt.Sub(now) > 9*time.Second {
		t.Fatalf("next skill delay = %s", vo.partySkillNextAt.Sub(now))
	}
	vo.resetPartyTQOSTransportUnsafe()
	if !vo.partyDungeonLastAt.IsZero() || !vo.partySkillNextAt.IsZero() || !vo.partySkillRecoverAt.IsZero() {
		t.Fatalf("dungeon skill state survived reset: last=%s next=%s recover=%s", vo.partyDungeonLastAt, vo.partySkillNextAt, vo.partySkillRecoverAt)
	}
}

func TestPartySkillRecoveryFailureBacksOffAndBlocksCast(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{
		UID:                  17000001,
		partySelfPeer:        partyIPPeer{uniqueID: 0x2f3e, slot: 1, slotKnown: true},
		partyDungeonLastAt:   now,
		partySkillRecoverAt:  now.Add(-time.Millisecond),
		partySkillNextAt:     now.Add(-time.Millisecond),
		partySkillLoaded:     true,
		partySkillCandidates: []partySkillCandidate{{skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}}},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, slot: 0, slotKnown: true, outerIP: net.IPv4(127, 0, 0, 1), port: 5063}

	vo.flushPartyDungeonSkillUnsafe(nil, now)
	if vo.partySkillRecoverAt != now.Add(partySkillRecoveryRetry) {
		t.Fatalf("recovery retry = %s, want %s", vo.partySkillRecoverAt, now.Add(partySkillRecoveryRetry))
	}
	if vo.partyTQOSReliableSeq[0][1] != 0 {
		t.Fatalf("failed recovery consumed reliable sequence: %d", vo.partyTQOSReliableSeq[0][1])
	}
	if !vo.partySkillNextAt.Before(now) {
		t.Fatalf("failed recovery unexpectedly rescheduled cast: %s", vo.partySkillNextAt)
	}

	vo.flushPartyDungeonSkillUnsafe(nil, now.Add(100*time.Millisecond))
	if vo.partyTQOSReliableSeq[0][1] != 0 || vo.partySkillRecoverAt != now.Add(partySkillRecoveryRetry) {
		t.Fatalf("recovery backoff was ignored: sequence=%d retry=%s", vo.partyTQOSReliableSeq[0][1], vo.partySkillRecoverAt)
	}
}

func flushQueuedDungeonFollow(t *testing.T, vo *RobotVo, frame []byte, peer partyIPPeer) []byte {
	t.Helper()
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
	remote := receiver.LocalAddr().(*net.UDPAddr)
	peer.outerIP = append(net.IP(nil), remote.IP...)
	peer.port = uint16(remote.Port)
	vo.partyPeers[peer.slot] = peer
	now := time.Unix(100, 0)
	if !vo.queuePartyDungeonFollowUnsafe(frame, peer, now) {
		t.Fatal("follow frame was not queued")
	}
	vo.flushPartyDungeonFollowUnsafe(sender, now.Add(vo.partyDungeonFollowDelayUnsafe()-time.Millisecond))
	if len(vo.partyDungeonFollow) != 1 {
		t.Fatalf("queue flushed early: %d", len(vo.partyDungeonFollow))
	}
	vo.flushPartyDungeonFollowUnsafe(sender, now.Add(vo.partyDungeonFollowDelayUnsafe()))
	buf := make([]byte, 4096)
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), buf[:n]...)
}

func TestPartyDungeonFrameCommandDetection(t *testing.T) {
	position := mustPartyHex(t, "028703000034000000015100970cfec701070034ede5df0001070034ede5df1102a97492965e0800000d000000ffffffffffffffff0000000000000000")
	if !partyDungeonFrameContainsCommand(position, 0x0051) || partyDungeonFrameContainsCommand(position, 0x0027) {
		t.Fatalf("position command detection failed")
	}
	reliable := []byte{0x02, 0x44, 0x00, 0xa7, 0xeb, 0x50, 0x2b, 0xec, 0xe8, 0x7e, 0x7e, 0x7e, 0x7e}
	frame := make([]byte, 11+len(reliable))
	frame[0] = 0x01
	binary.LittleEndian.PutUint16(frame[5:7], uint16(2+len(reliable)))
	binary.LittleEndian.PutUint16(frame[9:11], uint16(len(reliable)))
	copy(frame[11:], reliable)
	if !partyDungeonFrameContainsCommand(frame, 0x0044) || partyDungeonFrameContainsCommand(frame, 0x0051) {
		t.Fatalf("reliable command detection failed")
	}
}

func TestRememberPartyDungeonActivityFromReliableBatch(t *testing.T) {
	record := make([]byte, 11)
	record[0] = 0x02
	binary.LittleEndian.PutUint16(record[1:3], 0x0027)
	frame := make([]byte, 9+8*(2+len(record)))
	frame[0] = 0x01
	frame[7] = 0
	frame[8] = 5
	binary.LittleEndian.PutUint16(frame[5:7], uint16(len(frame)-9))
	offset := 9
	for range 8 {
		binary.LittleEndian.PutUint16(frame[offset:offset+2], uint16(len(record)))
		copy(frame[offset+2:], record)
		offset += 2 + len(record)
	}

	now := time.Unix(200, 0)
	peer := partyIPPeer{slot: 0, slotKnown: true, uniqueID: 0x1234}
	vo := &RobotVo{
		UID:           17000149,
		partySelfPeer: partyIPPeer{slot: 1, slotKnown: true, uniqueID: 0x5678},
	}
	vo.rememberPartyDungeonActivityUnsafe(frame, 1, peer, now)
	if vo.partyDungeonLastAt != now || vo.partyDungeonFlags != 5 || vo.partySkillNextAt.IsZero() {
		t.Fatalf("dungeon activity not scheduled: last=%s flags=%d next=%s", vo.partyDungeonLastAt, vo.partyDungeonFlags, vo.partySkillNextAt)
	}
}

func TestPartyDungeonSkillAndFollowUseRecentRelayRoute(t *testing.T) {
	relay, peerConn := net.Pipe()
	defer relay.Close()
	defer peerConn.Close()
	now := time.Now()
	vo := &RobotVo{
		UID:            17000001,
		partyRelayConn: relay,
		partySelfPeer:  partyIPPeer{uniqueID: 0x2f3e, accID: 17000001, slot: 1, slotKnown: true},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, accID: 18000000, slot: 0, slotKnown: true}
	vo.rememberPartyPeerRouteUnsafe(0, 2, now)

	skillPacket := make(chan []byte, 1)
	go func() { skillPacket <- readPartyRelayPacket(t, peerConn) }()
	if !vo.sendPartySkillStateUnsafe(nil, now, 22, []byte{3, 0, 0}, "TEST") {
		t.Fatal("relay skill send failed")
	}
	assertPartyRelayPayload(t, <-skillPacket, vo.UID, vo.partyPeers[0].accID)
	if vo.partyTQOSReliableSeq[0][2] != 1 || vo.partyTQOSReliableSeq[0][1] != 0 {
		t.Fatalf("skill route sequences=%+v", vo.partyTQOSReliableSeq[0])
	}

	vo.partyDungeonFollow = []partyDungeonFollowPending{{due: now, peerSlot: 0, flags: 1, body: []byte{1, 2, 3}}}
	followPacket := make(chan []byte, 1)
	go func() { followPacket <- readPartyRelayPacket(t, peerConn) }()
	vo.flushPartyDungeonFollowUnsafe(nil, now)
	assertPartyRelayPayload(t, <-followPacket, vo.UID, vo.partyPeers[0].accID)
	if vo.partyTQOSSeq[0][2] != 1 || vo.partyTQOSSeq[0][1] != 0 {
		t.Fatalf("follow route sequences=%+v", vo.partyTQOSSeq[0])
	}
}

func TestPartyDungeonSkillSurvivesRandomDelayOnRelay(t *testing.T) {
	relay, peerConn := net.Pipe()
	defer relay.Close()
	defer peerConn.Close()
	now := time.Now()
	vo := &RobotVo{
		UID:                  17000001,
		partyRelayConn:       relay,
		partySelfPeer:        partyIPPeer{uniqueID: 0x2f3e, accID: 17000001, slot: 1, slotKnown: true},
		partyDungeonLastAt:   now.Add(-5 * time.Second),
		partySkillNextAt:     now,
		partySkillLoaded:     true,
		partySkillCandidates: []partySkillCandidate{{skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}}},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, accID: 18000000, slot: 0, slotKnown: true}
	vo.rememberPartyPeerRouteUnsafe(0, 2, now)

	skillPacket := make(chan []byte, 1)
	go func() { skillPacket <- readPartyRelayPacket(t, peerConn) }()
	vo.flushPartyDungeonSkillUnsafe(nil, now)
	assertPartyRelayPayload(t, <-skillPacket, vo.UID, vo.partyPeers[0].accID)
	if vo.partySkillRecoverAt.IsZero() {
		t.Fatal("skill recovery was not scheduled")
	}
}

func TestPartyRelayOnlyLoopFlushesDungeonSkill(t *testing.T) {
	relay, peerConn := net.Pipe()
	defer peerConn.Close()
	now := time.Now()
	vo := &RobotVo{
		UID:                  17000001,
		State:                StateRun,
		partyRelayConn:       relay,
		partySelfPeer:        partyIPPeer{uniqueID: 0x2f3e, accID: 17000001, slot: 1, slotKnown: true},
		partyDungeonLastAt:   now,
		partySkillNextAt:     now.Add(-time.Millisecond),
		partySkillLoaded:     true,
		partySkillCandidates: []partySkillCandidate{{skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}}},
	}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, accID: 18000000, slot: 0, slotKnown: true}
	vo.rememberPartyPeerRouteUnsafe(0, 2, now)

	done := make(chan struct{})
	go func() {
		vo.partyRelayLoop(relay, vo.UID)
		close(done)
	}()
	_ = peerConn.SetReadDeadline(time.Now().Add(time.Second))
	packet := readPartyRelayPacket(t, peerConn)
	assertPartyRelayPayload(t, packet, vo.UID, vo.partyPeers[0].accID)
	vo.mu.Lock()
	recoveryScheduled := !vo.partySkillRecoverAt.IsZero()
	vo.mu.Unlock()
	if !recoveryScheduled {
		t.Fatal("relay-only flush did not schedule skill recovery")
	}

	vo.mu.Lock()
	vo.closePartyRelayUnsafe()
	vo.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay loop did not stop")
	}
}
