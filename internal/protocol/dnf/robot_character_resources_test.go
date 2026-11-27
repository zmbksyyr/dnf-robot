package dnf

import (
	"testing"
	"time"
)

func TestLoginResourcesFeedPaymentAndSurviveRoomAndDungeonChanges(t *testing.T) {
	info, ok := parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
	if !ok {
		t.Fatal("captured login")
	}
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	r.characterBaseInfo = info
	r.characterLevel = characterLevelInfo{known: true, level: 1}
	r.seedPartyResourcesUnsafe()
	actor := r.partyDungeonWorld.actors[self]
	// The simulation uses the server's base MP without equipment precision.
	if !actor.mpKnown || actor.mp != 140 {
		t.Fatalf("login pool = %+v", actor)
	}
	sender, _ := comboTestTransport(t, r)
	if !r.sendPartySkillCastUnsafe(sender, now, partySkillCandidate{skillIndex: 1, state: 20, level: 1, baseMPByLevel: []int{6}}) {
		t.Fatal("cast failed")
	}
	if r.characterResources.mp != 133 {
		t.Fatalf("login did not feed actual payment: %+v", r.characterResources)
	}
	r.markPartyDungeonEnteredUnsafe(now.Add(time.Second))
	if r.partyDungeonWorld.actors[self].mp != 133 {
		t.Fatal("room restored spent MP")
	}
	r.clearPartyDungeonRuntimeUnsafe()
	r.partySkillLoaded = true
	r.seedPartyResourcesUnsafe()
	if r.partyDungeonWorld.actors[self].mp != 133 {
		t.Fatal("dungeon reentry restored spent MP")
	}
	// The existing session reset is the only place that starts a new pool.
	r.characterResources = characterResourcePool{}
	r.partyDungeonWorld = partyDungeonWorld{}
	r.seedPartyResourcesUnsafe()
	if r.partyDungeonWorld.actors[self].mp != 140 {
		t.Fatal("new login did not initialize resources")
	}
}
