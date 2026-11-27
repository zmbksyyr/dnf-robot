package dnf

import (
	"testing"
	"time"
)

func TestPartySkillCandidateRotationIncludesEveryReadyTier(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{}
	vo.partySkillCandidates = []partySkillCandidate{
		{itemCostFree: true, skillIndex: 3, state: 22, level: 1, risk: 1},
		{itemCostFree: true, skillIndex: 4, state: 23, level: 1, risk: 1},
		{itemCostFree: true, skillIndex: 9, state: 90, level: 1, risk: 2},
		{itemCostFree: true, skillIndex: 10, state: 91, level: 1, risk: 2},
	}
	seen := make(map[byte]bool)
	for i := 0; i < 128; i++ {
		candidate, ok := vo.nextPartySkillCandidateUnsafe(now.Add(time.Duration(i) * time.Millisecond))
		if !ok {
			t.Fatalf("round %d found no candidate", i)
		}
		seen[candidate.skillIndex] = true
	}
	if len(seen) != 4 {
		t.Fatalf("ready skills were excluded by risk tier: %v", seen)
	}
}

func TestBasicAttackRecoveryDoesNotPostponeReadySkill(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.partySkillCandidates = []partySkillCandidate{{skillIndex: 20, state: 24, level: 7,
		stateData: []byte{20}, itemCostFree: true, cooldown: 3 * time.Second}}
	r.partySkillNextAt = now.Add(800 * time.Millisecond)
	sender, receiver := comboTestTransport(t, r)
	r.flushPartyDungeonSkillUnsafe(sender, now)
	readComboState(t, receiver, 8, 0, 2)
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
	// The active skill became due during the normal swing's recovery.
	recovery := now.Add(partySkillRecoverDelay)
	r.flushPartyDungeonSkillUnsafe(sender, recovery)
	readComboState(t, receiver, 0, 0, 1)
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 1), 1, r.partyPeers[0])
	r.flushPartyDungeonSkillUnsafe(sender, recovery.Add(100*time.Millisecond))
	readComboState(t, receiver, 24, 0, 2)
	if r.partySkillLastIndex != 20 || len(r.partyBasicProjectiles) != 1 {
		t.Fatal("normal attack recovery starved the ready active skill")
	}
}

func TestPartySkillCandidateAvoidsConsecutiveRepeats(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{partySkillLastIndex: 3, partySkillCandidates: []partySkillCandidate{
		{itemCostFree: true, skillIndex: 3, state: 22},
		{itemCostFree: true, skillIndex: 3, state: 23},
		{itemCostFree: true, skillIndex: 9, state: 90},
	}}
	for i := range 64 {
		candidate, ok := vo.nextPartySkillCandidateUnsafe(now.Add(time.Duration(i) * time.Millisecond))
		if !ok || candidate.skillIndex != 9 {
			t.Fatalf("repeated the last skill despite another ready skill: %+v", candidate)
		}
	}
	vo.partySkillReadyAt = map[byte]time.Time{9: now.Add(time.Minute)}
	if candidate, ok := vo.nextPartySkillCandidateUnsafe(now); !ok || candidate.skillIndex != 3 {
		t.Fatal("only ready skill must remain usable")
	}
}

func TestPartySkillCandidateRotationFallsThroughCoolingTiers(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{partySkillReadyAt: map[byte]time.Time{}}
	vo.partySkillCandidates = []partySkillCandidate{
		{itemCostFree: true, skillIndex: 3, state: 22, level: 1, risk: 1},
		{itemCostFree: true, skillIndex: 9, state: 90, level: 1, risk: 2},
	}
	vo.partySkillReadyAt[3] = now.Add(time.Minute)
	candidate, ok := vo.nextPartySkillCandidateUnsafe(now)
	if !ok || candidate.skillIndex != 9 {
		t.Fatalf("cooling verified tier = %+v ok=%t", candidate, ok)
	}
	// Once the fallback is cooling too, nothing is ready.
	vo.partySkillReadyAt[9] = now.Add(time.Minute)
	if _, ok := vo.nextPartySkillCandidateUnsafe(now); ok {
		t.Fatal("all-cooling candidates still reported ready")
	}
}

func TestPartySkillCandidateRotationSpreadsWithinTier(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{}
	vo.partySkillCandidates = []partySkillCandidate{
		{itemCostFree: true, skillIndex: 3, state: 22, level: 1, risk: 0},
		{itemCostFree: true, skillIndex: 4, state: 23, level: 1, risk: 0},
		{itemCostFree: true, skillIndex: 5, state: 24, level: 1, risk: 0},
	}
	seen := make(map[byte]bool)
	for i := 0; i < 64; i++ {
		candidate, ok := vo.nextPartySkillCandidateUnsafe(now.Add(time.Duration(i) * time.Second))
		if !ok {
			t.Fatalf("round %d found no candidate", i)
		}
		seen[candidate.skillIndex] = true
	}
	if len(seen) < 2 {
		t.Fatalf("rotation never varied inside the verified tier: %v", seen)
	}
}
