package dnf

import (
	"encoding/binary"
	"testing"
	"time"

	"robot/internal/shared"
)

func TestEquipmentBankDoesNotSeedCurrentResourcesAndObservedPaymentSurvivesRooms(t *testing.T) {
	info, ok := parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
	if !ok {
		t.Fatal("captured login")
	}
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	r.characterBaseInfo = info
	r.characterLevel = characterLevelInfo{known: true, level: 1}
	shared.SetResourceEquipment([]shared.EquipmentCatalogItem{
		{ID: 10400, Resources: &shared.EquipmentResources{}},
		{ID: 12400, Resources: &shared.EquipmentResources{}},
	})
	t.Cleanup(func() { shared.SetResourceEquipment(nil) })
	r.seedPartyResourcesUnsafe()
	actor := r.partyDungeonWorld.actors[self]
	if actor.mpKnown || actor.hpKnown || r.characterResources.equipmentMP != 142 {
		t.Fatalf("equipment bank became current resources: actor=%+v pool=%+v", actor, r.characterResources)
	}
	// A captured resource notification, rather than the incomplete equipment
	// bank, establishes the current MP from which payment can proceed.
	r.partyDungeonWorld.observe(buildPartyResourceBody(13, r.partySelfPeer.uniqueID, 639), now)
	r.rememberPartyResourcesUnsafe()
	sender, _ := comboTestTransport(t, r)
	if !r.sendPartySkillCastUnsafe(sender, now, partySkillCandidate{skillIndex: 1, state: 20, level: 1, baseMPByLevel: []int{6}, mpRules: shared.SkillMPRules{Known: true}}) {
		t.Fatal("cast failed")
	}
	if r.characterResources.mp != 633 {
		t.Fatalf("observation did not feed payment: %+v", r.characterResources)
	}
	r.markPartyDungeonEnteredUnsafe(now.Add(time.Second))
	if r.partyDungeonWorld.actors[self].mp != 633 {
		t.Fatal("room restored spent MP")
	}
	r.clearPartyDungeonRuntimeUnsafe()
	r.partySkillLoaded = true
	r.seedPartyResourcesUnsafe()
	if r.partyDungeonWorld.actors[self].mp != 633 {
		t.Fatal("dungeon reentry restored spent MP")
	}
	// A new login needs a fresh observation, not the previous session's MP.
	r.characterResources = characterResourcePool{}
	r.partyDungeonWorld = partyDungeonWorld{}
	r.seedPartyResourcesUnsafe()
	if r.partyDungeonWorld.actors[self].mpKnown {
		t.Fatal("new login retained or invented current MP")
	}
}

func TestPartySkillMPCostUsesPVFWeaponRateAndRejectsUnknownWeapon(t *testing.T) {
	weapon := make([]byte, 23)
	weapon[0] = 10
	binary.LittleEndian.PutUint32(weapon[1:5], 9001)
	shared.SetResourceEquipment([]shared.EquipmentCatalogItem{{ID: 9001, ItemType: 1, Slot: "weapon", SubType: 1, Resources: &shared.EquipmentResources{}}})
	t.Cleanup(func() { shared.SetResourceEquipment(nil) })
	r := &RobotVo{partySkillLoaded: true, partySkillJob: 0, characterLevel: characterLevelInfo{known: true, level: 1}}
	r.characterBaseInfo = characterBasicInfo{known: true, equipment: characterEquipmentInfo{known: true, slots: [26]string{10: string(weapon)}}}
	r.initializeCharacterResourcesUnsafe()
	if !r.characterResources.equipmentComplete || !r.characterResources.weaponTypeKnown {
		t.Fatalf("resource pool did not retain weapon type: %+v", r.characterResources)
	}
	physical := partySkillCandidate{level: 1, baseMPByLevel: []int{6}, mpRules: shared.SkillMPRules{Known: true, WeaponEffect: 1}}
	if got, ok := r.partySkillMPCostUnsafe(physical); !ok || got != 5 {
		t.Fatalf("physical weapon cost=%d known=%t, want 5/true", got, ok)
	}
	magical := partySkillCandidate{level: 1, baseMPByLevel: []int{17}, mpRules: shared.SkillMPRules{Known: true, WeaponEffect: 2}}
	if got, ok := r.partySkillMPCostUnsafe(magical); !ok || got != 17 {
		t.Fatalf("magical weapon cost=%d known=%t, want 17/true", got, ok)
	}
	r.characterBaseInfo.equipment.known = false
	if _, ok := r.partySkillMPCostUnsafe(physical); ok {
		t.Fatal("unknown weapon type produced an effective cost")
	}
}

func TestCharacterResourcePoolDoesNotInventMissingEquipment(t *testing.T) {
	info := characterBasicInfo{known: true, equipment: characterEquipmentInfo{known: true}}
	var record [23]byte
	record[0] = 10
	binary.LittleEndian.PutUint32(record[1:5], 9999)
	info.equipment.slots[10] = string(record[:])
	r := &RobotVo{partySkillLoaded: true, partySkillJob: 0, characterLevel: characterLevelInfo{known: true, level: 1}, characterBaseInfo: info}
	shared.SetResourceEquipment(nil)
	r.initializeCharacterResourcesUnsafe()
	if r.characterResources.equipmentComplete {
		t.Fatalf("missing equipment catalog became known pool: %+v", r.characterResources)
	}
}

func TestPostLoginUnknownSkillInputsDoNotCast(t *testing.T) {
	for _, tc := range []struct {
		name      string
		candidate partySkillCandidate
		mpKnown   bool
	}{
		{"missing base table", partySkillCandidate{mpRules: shared.SkillMPRules{Known: true}}, true},
		{"unknown definition rules", partySkillCandidate{baseMPByLevel: []int{6}}, true},
		{"unknown current MP", partySkillCandidate{baseMPByLevel: []int{6}, mpRules: shared.SkillMPRules{Known: true}}, false},
		{"unknown command mode", partySkillCandidate{baseMPByLevel: []int{6}, mpRules: shared.SkillMPRules{Known: true, CommandReductionPermille: 20}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			r, self, _ := basicAttackTestRobot(t, now)
			r.characterBaseInfo.known = true
			actor := r.partyDungeonWorld.actors[self]
			actor.mp, actor.mpKnown = 77, tc.mpKnown
			r.partyDungeonWorld.actors[self] = actor
			candidate := tc.candidate
			candidate.skillIndex, candidate.state, candidate.level, candidate.itemCostFree = 46, 28, 1, true
			r.partySkillCandidates = []partySkillCandidate{candidate}
			if _, ready := r.nextPartySkillCandidateUnsafe(now); ready {
				t.Fatal("selected a skill with unknown payment inputs")
			}
			sender, _ := comboTestTransport(t, r)
			if r.sendPartySkillCastUnsafe(sender, now, candidate) || r.partyDungeonWorld.actors[self].mp != 77 {
				t.Fatal("unknown payment was sent or changed MP")
			}
		})
	}
}

func TestWeaponReducedCostIsUsedForSkillAndStateSelection(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	r.characterBaseInfo = characterBasicInfo{known: true, equipment: characterEquipmentInfo{known: true}}
	r.characterLevel = characterLevelInfo{known: true, level: 1}
	weapon := make([]byte, 27)
	weapon[0] = 10
	binary.LittleEndian.PutUint32(weapon[1:5], 9001)
	r.characterBaseInfo.equipment.slots[10] = string(weapon)
	shared.SetResourceEquipment([]shared.EquipmentCatalogItem{{ID: 9001, ItemType: 1, SubType: 1, Resources: &shared.EquipmentResources{}}})
	t.Cleanup(func() { shared.SetResourceEquipment(nil) })
	actor := r.partyDungeonWorld.actors[self]
	actor.mp, actor.mpKnown = 5, true
	r.partyDungeonWorld.actors[self] = actor
	r.partySkillCandidates = []partySkillCandidate{
		{skillIndex: 46, state: 28, level: 1, itemCostFree: true, baseMPByLevel: []int{6}, mpRules: shared.SkillMPRules{Known: true, WeaponEffect: 1}},
		{skillIndex: 46, state: 29, level: 1, itemCostFree: true, baseMPByLevel: []int{6}, mpRules: shared.SkillMPRules{Known: true}},
	}
	for range 20 {
		chosen, ready := r.nextPartySkillCandidateUnsafe(now)
		if !ready || chosen.state != 28 {
			t.Fatalf("state selection disagrees with reduced cost: %+v ready=%t", chosen, ready)
		}
	}
	sender, _ := comboTestTransport(t, r)
	if !r.sendPartySkillCastUnsafe(sender, now, r.partySkillCandidates[0]) || r.partyDungeonWorld.actors[self].mp != 0 {
		t.Fatal("affordable reduced-cost cast did not spend exactly five MP")
	}
	// An equipment refresh must not retain the old weapon's reduction.
	r.characterBaseInfo.equipment.slots[10] = ""
	if cost, known := r.partySkillMPCostUnsafe(r.partySkillCandidates[0]); !known || cost != 6 {
		t.Fatalf("unarmed cost=%d known=%t, want 6/true", cost, known)
	}
	r.partySkillJob = 8
	if _, known := r.partySkillMPCostUnsafe(r.partySkillCandidates[0]); known {
		t.Fatal("swordman weapon rates were reused for another job")
	}
}

func TestLateEquipmentInitializationPreservesObservedResources(t *testing.T) {
	r := &RobotVo{partySkillLoaded: true, characterLevel: characterLevelInfo{known: true, level: 1},
		characterBaseInfo:  characterBasicInfo{known: true, equipment: characterEquipmentInfo{known: true}},
		characterResources: characterResourcePool{hp: 123, mp: 77, hpObserved: true, mpObserved: true, syncPending: true}}
	r.initializeCharacterResourcesUnsafe()
	if pool := r.characterResources; !pool.equipmentComplete || pool.hp != 123 || pool.mp != 77 || !pool.hpObserved || !pool.mpObserved || !pool.syncPending {
		t.Fatalf("late initialization overwrote observed state: %+v", pool)
	}
	r.characterBaseInfo.equipment.known = false
	r.initializeCharacterResourcesUnsafe()
	if pool := r.characterResources; pool.equipmentComplete || pool.weaponTypeKnown || pool.hp != 123 || pool.mp != 77 {
		t.Fatalf("refresh retained stale equipment or lost observations: %+v", pool)
	}
}

func TestEquipmentBankAppliesSelectedOptionSetAndEligibility(t *testing.T) {
	set := shared.EquipmentResourceSet{Items: []int{9001, 9002}, Pieces: 2, HP: 30, MP: 40}
	shared.SetResourceEquipment([]shared.EquipmentCatalogItem{
		{ID: 9001, Level: 1, UseJob: []int{0}, Durability: 20, Resources: &shared.EquipmentResources{HP: 10, MP: 20, Vitality: 250, Spirit: 250, Sets: []shared.EquipmentResourceSet{set}}},
		{ID: 9002, Resources: &shared.EquipmentResources{Options: []shared.ResourceStats{{HP: 1000}, {HP: 20, MP: 30}}, Sets: []shared.EquipmentResourceSet{set}}},
		{ID: 9003, Durability: 20, Resources: &shared.EquipmentResources{HP: 1000}},
		{ID: 9004, Level: 2, Resources: &shared.EquipmentResources{HP: 1000}},
		{ID: 9005, UseJob: []int{8}, Resources: &shared.EquipmentResources{HP: 1000}},
	})
	t.Cleanup(func() { shared.SetResourceEquipment(nil) })
	r := &RobotVo{partySkillLoaded: true, characterLevel: characterLevelInfo{known: true, level: 1},
		characterBaseInfo: characterBasicInfo{known: true, equipment: characterEquipmentInfo{known: true}}}
	binary.LittleEndian.PutUint32(r.characterBaseInfo.stats[0:4], 2000)
	binary.LittleEndian.PutUint32(r.characterBaseInfo.stats[4:8], 1000)
	for slot, id := range map[int]uint32{10: 9001, 0: 9002, 11: 9003, 12: 9004, 13: 9005} {
		record := make([]byte, 27)
		record[0] = byte(slot)
		binary.LittleEndian.PutUint32(record[1:5], id)
		if id == 9001 {
			binary.LittleEndian.PutUint16(record[10:12], 1)
		}
		if id == 9002 {
			binary.LittleEndian.PutUint32(record[16:20], 2)
		}
		r.characterBaseInfo.equipment.slots[slot] = string(record)
	}
	r.initializeCharacterResourcesUnsafe()
	if pool := r.characterResources; !pool.equipmentComplete || pool.equipmentHP != 520 || pool.equipmentMP != 380 || pool.hpObserved || pool.mpObserved {
		t.Fatalf("equipment option/set/eligibility bank = %+v", pool)
	}
}
