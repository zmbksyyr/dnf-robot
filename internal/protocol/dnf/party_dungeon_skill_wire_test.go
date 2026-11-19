package dnf

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestFollowRejectsCapturedMonsterControlledByLeader(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		leader uint16
	}{
		{"01040072b23aa47f6f7c5d546f7f797e7c7e6e012f3f7e3e793a7e7e393d5a848181", 7},
		{"015100a12a3fca010700677716ec00010700677716ec11026003a1f4f10200000d000000ffffffffffffffff0000000000000000", 0xf4a1},
	} {
		if _, _, ok := rewritePartyDungeonBody(mustPartyHex(t, tc.raw), tc.leader, 6); ok {
			t.Fatal("monster controller was rewritten as a player")
		}
	}
}

func TestFollowRejectsMonsterWithSameNumericIDAsLeader(t *testing.T) {
	body := mustPartyHex(t, "01040072b23aa47f6f7c5d546f7f797e7c7e6e012f3f7e3e793a7e7e393d5a848181")
	plain := append([]byte(nil), body[7:]...)
	for i := range plain {
		plain[i] ^= 0x7e
	}
	binary.LittleEndian.PutUint16(plain[3:5], 7)
	checksum := partyPayloadChecksum(plain)
	copy(body[3:7], checksum[:])
	for i, value := range plain {
		body[7+i] = value ^ 0x7e
	}
	if _, _, ok := rewritePartyDungeonBody(body, 7, 6); ok {
		t.Fatal("a monster reference collided with a player reference")
	}
}

func TestLegacySkillWireMatchesCapturedPlayerState(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		state byte
		data  []byte
		token uint16
	}{
		{"020500b7af53696f7f797e7d787e7e1a7e7e7a7e50b3", 3, []byte{0, 100, 0, 0, 4, 0}, 0xcd2e},
		{"0205008d8d1e0b6f7f797e7e7e7e7eb6", 0, nil, 0xc800},
	} {
		body := mustPartyHex(t, tc.raw)
		if got := buildPartyLegacySkillStateBody(7, tc.state, tc.data, tc.token); !bytes.Equal(got[:4], body[:4]) || !bytes.Equal(got[7:], body[7:]) {
			t.Fatalf("skill body=%x want capture=%x", got, body)
		}
		for _, frame := range [][]byte{buildPartyUnreliablePacket(1, 0, 0, body), buildPartyReliablePacket(1, 0, 0, [][]byte{body})} {
			vo := &RobotVo{partySelfPeer: partyIPPeer{slot: 1, slotKnown: true, uniqueID: 6}, partyDungeonEnteredAt: time.Now()}
			peer := partyIPPeer{slot: 0, slotKnown: true, uniqueID: 7}
			vo.rememberPartyDungeonActivityUnsafe(frame, 1, peer, time.Now())
			if !vo.partyDungeonLegacySkills || partyDungeonFrameUsesLegacySkills(frame, 8) {
				t.Fatal("legacy format selection did not use leader identity")
			}
			frame[len(frame)-1] ^= 1
			if partyDungeonFrameUsesLegacySkills(frame, 7) {
				t.Fatal("accepted corrupt checksum")
			}
		}
	}
}

func TestLearnedSkillTreeMatchesCapturedOriginalNotification(t *testing.T) {
	body := mustPartyHex(t, "11000d00a9010106010501010402180100032e0101040817010066b5020067b6020068b8010069b307006aae01006bac01006caa01006dc2090012000d00")
	levels, ok := parsePartyLearnedSkills(body)
	if !ok || levels[169] != 1 || levels[5] != 1 || levels[24] != 1 || levels[194] != 9 || levels[3] != 0 {
		t.Fatalf("learned=%v ok=%t", levels, ok)
	}
	// Every truncation inside the active tree must leave the old snapshot alone.
	for n := 0; n < 56; n++ {
		if _, valid := parsePartyLearnedSkills(body[:n]); valid {
			t.Fatalf("accepted truncated tree length=%d", n)
		}
	}
}

func TestLegacySkillWireMatchesCapturedAPCStates(t *testing.T) {
	// Same Aganzo cast: entry, preparation, then the 989 ms charged slash.
	for _, tc := range []struct {
		raw   string
		state byte
		data  []byte
		token uint16
	}{
		{"02050092df72316f7f7f35697c7e7e7f0073", 23, []byte{0, 1}, 0x0d7e},
		{"0205000d3d507e6f7f7f35877c7e7e7d54f0", 249, []byte{0, 3}, 0x8e2a},
		{"0205003e8d0c756f7f7f35877a7e7c7df923ed45", 249, []byte{2, 3, 0x87, 0x5d}, 0x3b93},
	} {
		body := mustPartyHex(t, tc.raw)
		got := buildPartyLegacySkillStateBody(0x4b01, tc.state, tc.data, tc.token)
		if !bytes.Equal(got[:4], body[:4]) || !bytes.Equal(got[7:], body[7:]) {
			t.Fatalf("APC state %d body=%x want capture=%x", tc.state, got, body)
		}
	}
}

func TestPartySkillRejectsEmptySlashBeforeTransport(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "envelope", true: "direct"}[legacy], func(t *testing.T) {
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
				UID: 17000001, partySkillJob: 0, partyDungeonLegacySkills: legacy,
				partySelfPeer: partyIPPeer{uniqueID: 0x2222, slot: 1, slotKnown: true},
			}
			vo.partyPeers[0] = partyIPPeer{uniqueID: 0x1111, slot: 0, slotKnown: true, outerIP: remote.IP, port: uint16(remote.Port)}
			if vo.sendPartySkillStateUnsafe(sender, now, 23, nil, "CAST") {
				t.Fatal("empty swordman slash was sent")
			}
			if vo.partyTQOSReliableSeq[0][1] != 0 || len(vo.partyReliablePending[0][1]) != 0 {
				t.Fatal("rejected slash changed reliable transport")
			}
			for i, state := range []byte{23, 0} {
				var data []byte
				if state == 23 {
					data = []byte{0, 1}
				}
				if !vo.sendPartySkillStateUnsafe(sender, now, state, data, "TEST") {
					t.Fatalf("valid state %d was rejected", state)
				}
				buf := make([]byte, 4096)
				if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				n, _, err := receiver.ReadFromUDP(buf)
				if err != nil {
					t.Fatal(err)
				}
				body := buildPartySkillStateBody(vo.partySelfPeer.uniqueID, state, data, partySkillToken(vo.UID, now))
				if legacy {
					body = buildPartyLegacySkillStateBody(vo.partySelfPeer.uniqueID, state, data, partySkillToken(vo.UID, now))
				}
				want := buildPartyReliablePacket(uint32(i), vo.partySelfPeer.slot, 0, [][]byte{body})
				if !bytes.Equal(buf[:n], want) {
					t.Fatalf("state %d packet=%x want=%x", state, buf[:n], want)
				}
				vo.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, uint32(i)), 1, vo.partyPeers[0])
			}
		})
	}
}
