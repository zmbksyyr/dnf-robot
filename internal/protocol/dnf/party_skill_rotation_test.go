package dnf

import (
	"testing"
	"time"
)

func TestPartySkillCandidateRotationPrefersVerifiedRiskTier(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{}
	// Candidates arrive sorted: risk 1 first, then risk 2.
	vo.partySkillCandidates = []partySkillCandidate{
		{skillIndex: 3, state: 22, level: 1, risk: 1},
		{skillIndex: 4, state: 23, level: 1, risk: 1},
		{skillIndex: 9, state: 90, level: 1, risk: 2},
		{skillIndex: 10, state: 91, level: 1, risk: 2},
	}
	for i := 0; i < 32; i++ {
		candidate, ok := vo.nextPartySkillCandidateUnsafe(now.Add(time.Duration(i) * time.Millisecond))
		if !ok {
			t.Fatalf("round %d found no candidate", i)
		}
		if candidate.risk != 1 {
			t.Fatalf("round %d picked risk %d (%+v) while a verified skill was ready", i, candidate.risk, candidate)
		}
	}
}

func TestPartySkillCandidateRotationFallsThroughCoolingTiers(t *testing.T) {
	now := time.Unix(300, 0)
	vo := &RobotVo{partySkillReadyAt: map[byte]time.Time{}}
	vo.partySkillCandidates = []partySkillCandidate{
		{skillIndex: 3, state: 22, level: 1, risk: 1},
		{skillIndex: 9, state: 90, level: 1, risk: 2},
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
		{skillIndex: 3, state: 22, level: 1, risk: 0},
		{skillIndex: 4, state: 23, level: 1, risk: 0},
		{skillIndex: 5, state: 24, level: 1, risk: 0},
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
