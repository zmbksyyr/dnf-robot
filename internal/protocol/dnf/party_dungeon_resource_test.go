package dnf

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestSkillPaymentTravelsWithStateAndReliableRetryDoesNotPayAgain(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	r.partySelfPeer.uniqueID = 0x19c
	actor := r.partyDungeonWorld.actors[self]
	delete(r.partyDungeonWorld.actors, self)
	self = partyDungeonPlayerReference(0x19c)
	actor.mp, actor.mpKnown = 623, true
	r.partyDungeonWorld.actors[self] = actor
	r.partySkillJob = 8
	r.partySkillNextAt = now
	r.partySkillCandidates = []partySkillCandidate{{skillIndex: 1, state: 20, level: 1,
		stateData: []byte{}, itemCostFree: true, baseMPByLevel: []int{6}, cooldown: time.Second}}
	sender, receiver := comboTestTransport(t, r)
	r.flushPartyDungeonSkillUnsafe(sender, now)
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	frame := append([]byte(nil), buf[:n]...)
	bodies, ok := partyDungeonFrameBodies(frame)
	if !ok || len(bodies) != 3 || binary.LittleEndian.Uint16(bodies[1][1:3]) != 25 {
		t.Fatalf("missing ordered facing/payment/state: %x", frame)
	}
	plain := append([]byte(nil), bodies[1][7:]...)
	for i := range plain {
		plain[i] ^= 0x7e
	}
	// Original USE_SKILL: actor11019c01 / skill1 / level1 / resultingMP617 / flag0.
	if want := mustPartyHex(t, "11019c0101016902000000"); !bytes.Equal(plain, want) {
		t.Fatalf("payment = %x, want %x", plain, want)
	}
	if got := r.partyDungeonWorld.actors[self]; got.mp != 617 || got.lastSkill != 1 {
		t.Fatalf("local payment not committed: %+v", got)
	}
	// Resend the transport's existing reliable frame, rather than invoking a
	// second cast. This is the loss/retry path used in real party sessions.
	r.flushPartyReliableUnsafe(sender, now.Add(time.Second))
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err = receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], frame) || r.partyDungeonWorld.actors[self].mp != 617 {
		t.Fatal("reliable retransmission changed the payment")
	}
}

func TestFailedSkillSendKeepsMPAndAffordableSkillSelection(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	actor := r.partyDungeonWorld.actors[self]
	actor.mp, actor.mpKnown = 5, true
	r.partyDungeonWorld.actors[self] = actor
	r.partySkillCandidates = []partySkillCandidate{
		{skillIndex: 1, state: 20, level: 1, itemCostFree: true, baseMPByLevel: []int{6}},
		{skillIndex: 2, state: 22, level: 1, itemCostFree: true, baseMPByLevel: []int{3}},
	}
	chosen, ok := r.nextPartySkillCandidateUnsafe(now)
	if !ok || chosen.skillIndex != 2 {
		t.Fatalf("unaffordable skill crowded out the usable skill: %+v", chosen)
	}
	sender, _ := comboTestTransport(t, r)
	sender.Close()
	if r.sendPartySkillCastUnsafe(sender, now, chosen) || r.partyDungeonWorld.actors[self].mp != 5 {
		t.Fatal("failed send paid MP")
	}
}
