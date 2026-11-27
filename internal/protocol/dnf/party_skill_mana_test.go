package dnf

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestObservedMPExhaustionFallsBackToNormalAttackAndResumesAfterRecovery(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	r.partySkillNextAt = now
	r.partySkillCandidates = []partySkillCandidate{{skillIndex: 5, state: 20, level: 2,
		stateData: []byte{0, 0}, itemCostFree: true, baseMPByLevel: []int{0, 1}}}
	// The same SET_MP decoder used for a live resource notification must drive
	// selection. Do not initialize MP from the base attributes.
	resource := make([]byte, 8)
	copy(resource, self[:])
	r.partyDungeonWorld.observeActorEvent(13, resource, now)
	sender, receiver := comboTestTransport(t, r)
	r.flushPartyDungeonSkillUnsafe(sender, now)
	readComboState(t, receiver, 8, 0, 2)
	if !r.partySkillReadyAt[5].IsZero() || r.partySkillLastIndex != 0 {
		t.Fatal("zero MP consumed the active skill cooldown")
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
	r.partyBasicAttackStep = 0
	r.partySkillRecoverAt = time.Time{}
	r.partySkillNextAt = now.Add(time.Second)
	binary.LittleEndian.PutUint32(resource[4:], 10)
	r.partyDungeonWorld.observeActorEvent(13, resource, now.Add(time.Second))
	r.flushPartyDungeonSkillUnsafe(sender, now.Add(time.Second))
	readComboState(t, receiver, 20, 0, 3)
	if r.partySkillLastIndex != 5 {
		t.Fatal("observed MP recovery did not resume active casting")
	}
	if r.partyDungeonWorld.actors[self].mp != 9 {
		t.Fatal("resumed skill did not pay its MP cost")
	}
}

func TestZeroMPKeepsFreeSkillsAndUsesTheirLearnedLevel(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	actor := r.partyDungeonWorld.actors[self]
	actor.mpKnown = true
	r.partyDungeonWorld.actors[self] = actor
	r.partySkillCandidates = []partySkillCandidate{
		{skillIndex: 5, state: 20, level: 2, itemCostFree: true, baseMPByLevel: []int{0, 1}},
		{skillIndex: 46, state: 28, level: 2, itemCostFree: true, baseMPByLevel: []int{1, 0}},
	}
	got, ok := r.nextPartySkillCandidateUnsafe(now)
	if !ok || got.skillIndex != 46 {
		t.Fatalf("free skill was crowded out by an unaffordable skill: %+v, %t", got, ok)
	}
}
