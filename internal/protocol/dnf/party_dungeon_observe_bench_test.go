package dnf

import (
	"testing"
	"time"
)

// benchDungeonFrame builds the kind of reliable frame a leader sends: one motion
// body with leader, robot and monster, plus a legacy skill state body.
func benchDungeonFrame(b *testing.B) []byte {
	b.Helper()
	leader := partyDungeonPlayerReference(7)
	robot := partyDungeonPlayerReference(8)
	monster := [4]byte{0x11, 2, 1, 3}
	entries := []partyDungeonMotion{
		{entity: leader, controller: leader, modes: 0x415555, flags: [3]byte{2, 0, 0x10}},
		{entity: robot, controller: robot, modes: 0x415555, flags: [3]byte{2, 0, 0x10}},
		{entity: monster, controller: monster, modes: 0x415555, flags: [3]byte{2, 0, 0x10}},
	}
	for i := range entries {
		entries[i].values[9] = 0
	}
	motion, ok := buildPartyDungeonMotion(entries)
	if !ok {
		b.Fatal("build motion")
	}
	skill := buildPartyLegacySkillStateBody(7, 8, []byte{0}, 1)
	body := make([]byte, 0, 2+len(motion)+2+len(skill))
	size := make([]byte, 2)
	size[0] = byte(len(motion))
	size[1] = byte(len(motion) >> 8)
	body = append(body, size...)
	body = append(body, motion...)
	size[0] = byte(len(skill))
	size[1] = byte(len(skill) >> 8)
	body = append(body, size...)
	body = append(body, skill...)
	frame := make([]byte, 9+len(body))
	frame[0] = 1
	frame[5] = byte(len(body))
	frame[6] = byte(len(body) >> 8)
	frame[8] = 0
	copy(frame[9:], body)
	return frame
}

func benchDungeonRobot() *RobotVo {
	vo := &RobotVo{
		UID:                 17000021,
		partySelfPeer:       partyIPPeer{slot: 1, slotKnown: true, uniqueID: 8},
		partyPeers:          [4]partyIPPeer{{slot: 0, slotKnown: true, uniqueID: 7, accID: 17000015}},
		partyDungeonFlags:   0,
		partyDungeonWorld:   partyDungeonWorld{},
		partyDungeonEnteredAt: time.Unix(1000, 0),
	}
	return vo
}

func BenchmarkObservePartyDungeonFrame(b *testing.B) {
	frame := benchDungeonFrame(b)
	vo := benchDungeonRobot()
	peer := vo.partyPeers[0]
	now := time.Unix(1000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vo.partyDungeonLastAt = now.Add(time.Duration(i) * time.Millisecond)
		vo.observePartyDungeonFrameUnsafe(frame, peer, now.Add(time.Duration(i)*time.Millisecond))
	}
}

func BenchmarkPartyDungeonFrameBodies(b *testing.B) {
	frame := benchDungeonFrame(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := partyDungeonFrameBodies(frame); !ok {
			b.Fatal("frame rejected")
		}
	}
}
