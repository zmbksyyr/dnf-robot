package dnf

import (
	"net"
	"testing"
	"time"

	"robot/internal/shared"
)

func TestFailedDungeonSkillSendDoesNotStartCooldown(t *testing.T) {
	now := time.Unix(200, 0)
	r := &RobotVo{
		UID: 42, partySelfPeer: partyIPPeer{uniqueID: 2, slot: 1, slotKnown: true},
		partyDungeonEnteredAt: now, partyDungeonLastAt: now, partySkillNextAt: now.Add(-time.Millisecond),
		partySkillLoaded: true, partySkillJob: 8,
		partySkillCandidates: []partySkillCandidate{{skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}, cooldown: 15 * time.Second}},
	}
	r.partyPeers[0] = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true, outerIP: net.IPv4(127, 0, 0, 1), port: 5063}
	r.partyDungeonWorld = skillTargetTestWorld(r.partySelfPeer.uniqueID, now)
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	sender.Close()
	r.flushPartyDungeonSkillUnsafe(sender, now)
	if len(r.partySkillReadyAt) != 0 || !r.partySkillRecoverAt.IsZero() {
		t.Fatal("failed cast started cooldown or recovery")
	}
}

func TestSkillSelectionSharesCooldownAcrossStatesAndPreservesItAcrossRooms(t *testing.T) {
	now := time.Now()
	r := &RobotVo{UID: 42,
		partyDungeonEnteredAt: now, partyDungeonLastAt: now,
		partySkillCandidates: []partySkillCandidate{{skillIndex: 9, state: 23}, {skillIndex: 9, state: 249}, {skillIndex: 3, state: 22}},
		partySkillReadyAt:    map[byte]time.Time{9: now.Add(15 * time.Second)},
	}
	for range 30 {
		c, ok := r.nextPartySkillCandidateUnsafe(now)
		if !ok || c.skillIndex != 3 {
			t.Fatalf("selected cooling skill through another state: %+v %t", c, ok)
		}
	}
	r.partySkillReadyAt[3] = now.Add(2 * time.Second)
	if _, ok := r.nextPartySkillCandidateUnsafe(now); ok {
		t.Fatal("selected a skill while every candidate was cooling")
	}
	r.clearPartyDungeonRuntimeUnsafe()
	if _, ok := r.nextPartySkillCandidateUnsafe(now); ok {
		t.Fatal("leaving a room erased skill cooldowns")
	}
	if c, ok := r.nextPartySkillCandidateUnsafe(now.Add(2 * time.Second)); !ok || c.skillIndex != 3 {
		t.Fatal("did not resume an expired cooldown")
	}
	r.finishReturnToSelectUnsafe()
	if r.partySkillReadyAt != nil {
		t.Fatal("character selection retained the previous character's cooldowns")
	}
}

func TestSkillCandidateGetsBaselineFromPVFMatchingPath(t *testing.T) {
	whitelist := []shared.PartySkillState{{Job: 0, SkillIndex: 9, State: 249, Level: 1, ScriptPath: "sqr/right.nut"}}
	pvf := []shared.SkillState{
		{Job: 0, SkillIndex: 9, State: 249, ScriptPath: "sqr/wrong.nut", CooldownMS: 30000},
		{Job: 0, SkillIndex: 9, State: 249, ScriptPath: "sqr/right.nut", CooldownMS: 15000},
	}
	got, _ := partySkillCandidatesFromCatalog(0, whitelist, pvf)
	if len(got) != 1 || got[0].cooldown != 15*time.Second {
		t.Fatalf("candidate used another script's cooldown: %+v", got)
	}
}

func TestSkillCooldownUsesLearnedLevelAfterProfileClamp(t *testing.T) {
	whitelist := []shared.PartySkillState{{Job: 8, SkillIndex: 26, State: 43, Level: 70, ScriptPath: "sqr/test.nut"}}
	pvf := []shared.SkillState{{Job: 8, SkillIndex: 26, State: 43, ScriptPath: "sqr/test.nut", CooldownByLevelMS: []int{10000, 8000, 6000}}}
	candidates, _ := partySkillCandidatesFromCatalog(8, whitelist, pvf)
	r := &RobotVo{UID: 42, State: StateRun}
	r.partySkillLevels[26] = 2
	r.loadPartySkillProfile(0, shared.PartySkillStateGeneration(), r.UID, 1, func(uint32, int) (partySkillProfile, error) {
		return partySkillProfile{cid: 1, job: 8, candidates: candidates}, nil
	})
	if len(r.partySkillCandidates) != 1 || r.partySkillCandidates[0].level != 2 || r.partySkillCandidates[0].cooldownAtLevel() != 8*time.Second {
		t.Fatalf("cooldown was resolved before the server's learned level: %+v", r.partySkillCandidates)
	}
	pvf[0].CooldownByLevelMS[1] = 1
	if r.partySkillCandidates[0].cooldownAtLevel() != 8*time.Second {
		t.Fatal("runtime cooldown aliases mutable catalog storage")
	}
}
