package dnf

import (
	"testing"
	"time"

	"robot/internal/shared"
)

func TestPartySkillSelectionSkipsUnresolvedMaterialConsumption(t *testing.T) {
	now := time.Unix(500, 0)
	r := &RobotVo{partySkillCandidates: []partySkillCandidate{
		{skillIndex: 3, state: 22, risk: 0},
		{skillIndex: 4, state: 23, risk: 1, itemCostFree: true},
	}}
	for i := range 32 {
		candidate, ok := r.nextPartySkillCandidateUnsafe(now.Add(time.Duration(i) * time.Millisecond))
		if !ok || candidate.skillIndex != 4 {
			t.Fatalf("selected unresolved material cost: %+v, ready=%t", candidate, ok)
		}
	}
	r.partySkillReadyAt = map[byte]time.Time{4: now.Add(time.Minute)}
	if _, ok := r.nextPartySkillCandidateUnsafe(now); ok {
		t.Fatal("cooldown enabled a skill without material consumption")
	}
}

func TestPartySkillMaterialProofReachesRuntimeFromPVF(t *testing.T) {
	for _, free := range []bool{false, true} {
		whitelist := []shared.PartySkillState{{Job: 0, SkillIndex: 46, State: 28, Level: 10, StateData: []byte{2, 46}, ItemCostFree: true}}
		pvf := []shared.SkillState{{Job: 0, SkillIndex: 46, State: 28, ScriptPath: "skill/swordman/upperslash.skl", ItemCostFree: free}}
		candidates, _ := partySkillCandidatesFromCatalogMode(true, 0, whitelist, pvf)
		r := &RobotVo{UID: 42, State: StateRun}
		r.partySkillLevels[46] = 1
		r.loadPartySkillProfile(0, shared.PartySkillStateGeneration(), r.UID, 1, func(uint32, int) (partySkillProfile, error) {
			return partySkillProfile{cid: 1, job: 0, candidates: candidates}, nil
		})
		candidate, ok := r.nextPartySkillCandidateUnsafe(time.Now())
		if ok != free || free && (candidate.skillIndex != 46 || candidate.level != 1) {
			t.Fatalf("material proof did not survive profile loading: %+v, ready=%t, free=%t", candidate, ok, free)
		}
	}
}

func TestPartySkillMaterialGateControlsActualCastTransport(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "envelope", true: "legacy"}[legacy], func(t *testing.T) {
			now := time.Now()
			r, _, _ := basicAttackTestRobot(t, now)
			r.partyDungeonLegacySkills = legacy
			r.partySkillJob = 8
			r.partySkillNextAt = now
			r.partyBasicAttackNextAt = now.Add(time.Hour)
			r.partySkillCandidates = []partySkillCandidate{{skillIndex: 3, state: 22, risk: 0}}
			sender, receiver := comboTestTransport(t, r)
			r.flushPartyDungeonSkillUnsafe(sender, now)
			if r.partyTQOSReliableSeq[0][1] != 0 || !r.partySkillRecoverAt.IsZero() || len(r.partySkillReadyAt) != 0 {
				t.Fatal("unresolved material consumption reached cast transport")
			}
			r.partySkillCandidates = append(r.partySkillCandidates, partySkillCandidate{
				skillIndex: 4, state: 24, risk: 1, itemCostFree: true, cooldown: time.Second,
			})
			r.partySkillNextAt = now
			r.flushPartyDungeonSkillUnsafe(sender, now)
			if r.partySkillReadyAt[4] != now.Add(time.Second) || r.partySkillRecoverAt.IsZero() {
				t.Fatal("material gate prevented the available free skill from casting")
			}
			buf := make([]byte, 4096)
			receiver.SetReadDeadline(time.Now().Add(time.Second))
			n, _, err := receiver.ReadFromUDP(buf)
			if err != nil {
				t.Fatal(err)
			}
			bodies, ok := partyDungeonFrameBodies(buf[:n])
			if !ok || len(bodies) != 2 {
				t.Fatalf("free skill did not reach reliable UDP transport: %x", buf[:n])
			}
		})
	}
}

func TestPartySkillPVFReloadInvalidatesCachedMaterialProof(t *testing.T) {
	for _, replacement := range [][]shared.SkillState{nil, {{Job: 0, SkillIndex: 46, State: 28}}} {
		shared.SetSkillStates([]shared.SkillState{{Job: 0, SkillIndex: 46, State: 28, ItemCostFree: true}})
		r := &RobotVo{partySkillLoaded: true, partySkillCatalogGeneration: shared.PartySkillStateGeneration(),
			partySkillCandidates: []partySkillCandidate{{skillIndex: 46, state: 28, itemCostFree: true}},
		}
		shared.SetSkillStates(replacement)
		if r.ensurePartySkillProfileUnsafe() || r.partySkillLoaded || len(r.partySkillCandidates) != 0 {
			t.Fatal("replaced PVF retained cached material proof")
		}
	}
	t.Cleanup(func() { shared.SetSkillStates(nil) })
}

func TestPartySkillMaterialDefinitionsReachLearnedRuntimeWithoutEnablingConsumption(t *testing.T) {
	definition := shared.SkillMaterialDefinition{Known: true, MaximumLevel: 70, Blocks: []shared.SkillMaterialBlock{
		{Mode: "common", Rows: []shared.SkillMaterialRow{{ItemID: 3037, Start: 1, End: 3}}},
	}}
	pvf := []shared.SkillState{{Job: 0, SkillIndex: 9, State: 23, Materials: definition}}
	candidates, _ := partySkillCandidatesFromCatalogMode(true, 0,
		[]shared.PartySkillState{{Job: 0, SkillIndex: 9, State: 23, Level: 10, StateData: []byte{0, 1}}}, pvf)
	r := &RobotVo{UID: 42, State: StateRun}
	r.partySkillLevels[9] = 4
	r.loadPartySkillProfile(0, shared.PartySkillStateGeneration(), r.UID, 1, func(uint32, int) (partySkillProfile, error) {
		return partySkillProfile{cid: 1, job: 0, candidates: candidates}, nil
	})
	if len(r.partySkillCandidates) != 1 || r.partySkillCandidates[0].level != 4 ||
		!r.partySkillCandidates[0].materials.Equal(definition) {
		t.Fatal("PVF material definition did not reach learned runtime candidate", r.partySkillCandidates)
	}
	definition.Blocks[0].Rows[0].Start = 999
	if r.partySkillCandidates[0].materials.Blocks[0].Rows[0].Start != 1 {
		t.Fatal("runtime material candidate aliases source catalog")
	}
	if _, ok := r.nextPartySkillCandidateUnsafe(time.Now()); ok {
		t.Fatal("base material definition was treated as a resolved effective cost")
	}
}
