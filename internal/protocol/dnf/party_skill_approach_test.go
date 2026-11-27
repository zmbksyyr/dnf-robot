package dnf

import (
	"testing"
	"time"
)

func TestUppercutWaitsForApproachThenCastsWithoutRandomInterval(t *testing.T) {
	for _, skill := range []struct {
		name         string
		job          int
		index, state byte
		data         []byte
	}{
		{"swordman", 0, 46, 28, []byte{2, 46}},
		{"gunner", 2, 4, 20, []byte{4}},
		{"mage", 3, 12, 23, []byte{12}},
	} {
		t.Run(skill.name, func(t *testing.T) {
			now := time.Now()
			r, self, enemy := basicAttackTestRobot(t, now)
			r.partySkillJob = skill.job
			r.partySkillCandidates = []partySkillCandidate{{skillIndex: skill.index, state: skill.state,
				stateData: skill.data, itemCostFree: true, cooldown: 2 * time.Second}}
			r.partySkillNextAt = now
			// Windows swordman session: the first uppercut was cast before
			// approaching from x=1081 to the enemies around x=790.
			owner, target := r.partyDungeonWorld.actors[self], r.partyDungeonWorld.actors[enemy]
			owner.position.x, owner.position.y = 1081, 236
			target.position.x, target.position.y = 790, 229
			r.partyDungeonWorld.actors[self], r.partyDungeonWorld.actors[enemy] = owner, target
			sender, receiver := comboTestTransport(t, r)
			r.flushPartyDungeonSkillUnsafe(sender, now)
			if !r.partySkillRecoverAt.IsZero() || len(r.partySkillReadyAt) != 0 || r.partyReliablePurposePendingUnsafe(0, "skill-") {
				t.Fatal("distant uppercut consumed a cast or blocked approach")
			}
			// Arrival happens well before the normal 4-6 second skill interval.
			owner.position.x = 860
			r.partyDungeonWorld.actors[self] = owner
			arrival := now.Add(250 * time.Millisecond)
			r.flushPartyDungeonSkillUnsafe(sender, arrival)
			readComboState(t, receiver, skill.state, 0, 2)
			if r.partySkillReadyAt[skill.index] != arrival.Add(2*time.Second) || r.partySkillRecoverAt.IsZero() {
				t.Fatal("arrival did not execute the waiting uppercut")
			}
		})
	}
}
