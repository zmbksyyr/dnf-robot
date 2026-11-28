package dnf

import (
	"encoding/binary"
	"fmt"
	"strings"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

// Current resources come only from observed dungeon state or a committed
// USE_SKILL payment. The equipment bank is an intermediate calculation: it
// excludes passive/runtime modifiers and cannot initialize current HP/MP.
type characterResourcePool struct {
	hp, mp                int32
	equipmentHP           int32
	equipmentMP           int32
	definitionHP          int32
	definitionMP          int32
	titlebookDefaultKnown bool
	titlebook             characterTitlebookInfo
	definitionGeneration  uint64
	hpObserved            bool
	mpObserved            bool
	vitality, spirit      int32
	hpRate, mpRate        float32
	mpRegen               int32
	mpRegenKnown          bool
	weaponType            int
	weaponTypeKnown       bool
	equipmentComplete     bool
	equipmentSource       characterBasicInfo
	equipmentLevel        characterLevelInfo
	equipmentJob          int
	syncPending           bool
}

func (r *RobotVo) initializeCharacterResourcesUnsafe() {
	info := r.characterBaseInfo
	pool := &r.characterResources
	generation := shared.CharacterResourceGeneration()
	if pool.equipmentComplete && pool.equipmentSource == info && pool.equipmentLevel == r.characterLevel && pool.equipmentJob == r.partySkillJob && pool.definitionGeneration == generation && r.partySkillLoaded {
		return
	}
	pool.equipmentComplete, pool.weaponTypeKnown = false, false
	pool.titlebookDefaultKnown = false
	pool.definitionHP, pool.definitionMP = 0, 0
	if !info.known || !info.equipment.known || !r.characterLevel.known || !r.partySkillLoaded {
		return
	}
	hp := int(binary.LittleEndian.Uint32(info.stats[0:4]) / 10)
	mp := int(info.baseMP())
	vitality := int(binary.LittleEndian.Uint16(info.stats[0xa:0xc]) / 10)
	spirit := int(binary.LittleEndian.Uint16(info.stats[0xe:0x10]) / 10)
	hpRate, mpRate := float32(1), float32(1)
	equipped := make(map[int]bool)
	var sets []shared.EquipmentResourceSet
	weaponType, weaponTypeKnown := 0, false
	add := func(stat shared.ResourceStats) {
		hp += stat.HP
		mp += stat.MP
		vitality += stat.Vitality
		spirit += stat.Spirit
		hpRate += stat.HPRate
		mpRate += stat.MPRate
	}
	for slot, record := range info.equipment.slots {
		id := int(info.equipment.itemID(slot))
		if id == 0 {
			continue
		}
		item, ok := shared.ResourceEquipment(id)
		if !ok || item.Resources == nil {
			foundationlog.Robotf("[CHARACTER_RESOURCE_SKIP] uid=%d cid=%d reason=equipment_resource_unknown slot=%d item=%d\n", r.UID, r.CID, slot, id)
			return
		}
		if item.Level > int(r.characterLevel.level) {
			continue
		}
		if len(item.UseJob) > 0 {
			usable := false
			for _, job := range item.UseJob {
				if job == 100 || job == r.partySkillJob {
					usable = true
					break
				}
			}
			if !usable {
				continue
			}
		}
		if item.Durability > 0 {
			endurance, known := info.equipment.nativeEndurance(slot)
			if !known {
				foundationlog.Robotf("[CHARACTER_RESOURCE_SKIP] uid=%d cid=%d reason=equipment_endurance_unknown slot=%d item=%d\n", r.UID, r.CID, slot, id)
				return
			}
			if endurance == 0 {
				continue
			}
		}
		equipped[id] = true
		res := item.Resources
		add(shared.ResourceStats{HP: res.HP, MP: res.MP, Vitality: res.Vitality, Spirit: res.Spirit, HPRate: res.HPRate, MPRate: res.MPRate})
		if slot < 10 && len(res.Options) > 0 {
			// Avatars use the enchant DWORD for their one-based selected
			// ability. A malformed record cannot be treated as option zero.
			if len(record) < 20 {
				foundationlog.Robotf("[CHARACTER_RESOURCE_SKIP] uid=%d cid=%d reason=avatar_option_unknown slot=%d item=%d\n", r.UID, r.CID, slot, id)
				return
			}
			option := int(binary.LittleEndian.Uint32([]byte(record[16:20]))) - 1
			if option >= 0 {
				if option >= len(res.Options) {
					foundationlog.Robotf("[CHARACTER_RESOURCE_SKIP] uid=%d cid=%d reason=avatar_option_out_of_range slot=%d item=%d option=%d\n", r.UID, r.CID, slot, id, option+1)
					return
				}
				add(res.Options[option])
			}
		}
		if item.ItemType == 1 || strings.EqualFold(item.Slot, "weapon") {
			if weaponTypeKnown {
				foundationlog.Robotf("[CHARACTER_RESOURCE_SKIP] uid=%d cid=%d reason=multiple_weapons\n", r.UID, r.CID)
				return
			}
			if item.SubType < 0 {
				foundationlog.Robotf("[CHARACTER_RESOURCE_SKIP] uid=%d cid=%d reason=weapon_type_unknown item=%d subtype=%d\n", r.UID, r.CID, id, item.SubType)
				return
			}
			weaponType, weaponTypeKnown = item.SubType, true
		}
		sets = append(sets, res.Sets...)
	}
	appliedSets := make(map[string]bool)
	for _, set := range sets {
		key := fmt.Sprint(set.Items, ":", set.Pieces)
		if appliedSets[key] {
			continue
		}
		count := 0
		for _, id := range set.Items {
			if equipped[id] {
				count++
			}
		}
		if set.Pieces > 0 && count >= set.Pieces {
			appliedSets[key] = true
			add(shared.ResourceStats{HP: set.HP, MP: set.MP, Vitality: set.Vitality, Spirit: set.Spirit})
		}
	}
	// The native final bank adds the primary-stat term to the equipment rate.
	maxHP := max(1, int32(float32(hp)*float32(hpRate+float32(vitality)/250)))
	maxMP := max(1, int32(float32(mp)*float32(mpRate+float32(spirit)/250)))
	regen, regenKnown := info.baseMPRegen(r.partySkillJob)
	// Preserve earlier observations and a pending room sync if PVF/profile
	// loading finished after the first resource packet.
	pool.equipmentHP, pool.equipmentMP = maxHP, maxMP
	pool.vitality, pool.spirit = int32(vitality), int32(spirit)
	pool.hpRate, pool.mpRate = hpRate, mpRate
	pool.mpRegen, pool.mpRegenKnown = regen, regenKnown
	// No active weapon uses the unarmed entry (index zero).
	pool.weaponType, pool.weaponTypeKnown = weaponType, true
	pool.equipmentComplete = true
	pool.equipmentSource, pool.equipmentLevel, pool.equipmentJob = info, r.characterLevel, r.partySkillJob
	pool.definitionGeneration = generation
	if bonus, known := shared.TitlebookDefaultResourceBonus(); known {
		// Default rewards apply even to empty title collections. Collection,
		// passive and runtime effects remain separate inputs, so this bank is
		// still not a final pool or a source of current HP/MP.
		vit := max(0, int32(float32(int32(vitality)+bonus.Vitality)*float32(1+bonus.VitalityRate)))
		spi := max(0, int32(float32(int32(spirit)+bonus.Spirit)*float32(1+bonus.SpiritRate)))
		finalHPRate := float32(hpRate+bonus.HPRate) + float32(float32(vit)/250)
		finalMPRate := float32(mpRate+bonus.MPRate) + float32(float32(spi)/250)
		pool.definitionHP = max(1, int32(float32(int32(hp)+bonus.HP)*finalHPRate))
		pool.definitionMP = max(1, int32(float32(int32(mp)+bonus.MP)*finalMPRate))
		pool.titlebookDefaultKnown = true
	}
	foundationlog.Robotf("[CHARACTER_RESOURCE_EQUIPMENT] uid=%d cid=%d source=pvf_equipment hp=%d mp=%d vitality=%d spirit=%d hp_rate=%g mp_rate=%g weapon_type=%d mp_regen=%d mp_regen_known=%t titlebook_default_known=%t definition_hp=%d definition_mp=%d final_resources_known=false\n", r.UID, r.CID, maxHP, maxMP, vitality, spirit, hpRate, mpRate, weaponType, regen, regenKnown, pool.titlebookDefaultKnown, pool.definitionHP, pool.definitionMP)
}

func (r *RobotVo) seedPartyResourcesUnsafe() {
	r.initializeCharacterResourcesUnsafe()
	if r.partySelfPeer.uniqueID == 0 {
		return
	}
	if r.partyDungeonWorld.actors == nil {
		r.partyDungeonWorld.actors = make(map[[4]byte]partyDungeonActor)
	}
	ref := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	actor := r.partyDungeonWorld.actors[ref]
	if !actor.hpKnown && r.characterResources.hpObserved {
		actor.hp, actor.hpKnown = r.characterResources.hp, true
	}
	if !actor.mpKnown && r.characterResources.mpObserved {
		actor.mp, actor.mpKnown = r.characterResources.mp, true
	}
	r.partyDungeonWorld.actors[ref] = actor
}

func (r *RobotVo) rememberPartyResourcesUnsafe() {
	if r.partySelfPeer.uniqueID == 0 {
		return
	}
	actor := r.partyDungeonWorld.actors[partyDungeonPlayerReference(r.partySelfPeer.uniqueID)]
	if actor.hpKnown {
		r.characterResources.syncPending = r.characterResources.syncPending || !r.characterResources.hpObserved || r.characterResources.hp != actor.hp
		r.characterResources.hp = actor.hp
		r.characterResources.hpObserved = true
	}
	if actor.mpKnown {
		r.characterResources.syncPending = r.characterResources.syncPending || !r.characterResources.mpObserved || r.characterResources.mp != actor.mp
		r.characterResources.mp = actor.mp
		r.characterResources.mpObserved = true
	}
}

// partySkillMPCostUnsafe returns a cost only when every definition input used
// by this client-side payment is known. It deliberately has no max-MP or other
// percentage fallback.
func (r *RobotVo) partySkillMPCostUnsafe(candidate partySkillCandidate) (int32, bool) {
	if len(candidate.baseMPByLevel) == 0 {
		// Before USERINFO, preserve legacy action-only traffic without claiming
		// a resource payment. A logged-in character requires an explicit table.
		return 0, !r.characterBaseInfo.known
	}
	level := min(max(candidate.level, 1), len(candidate.baseMPByLevel))
	base := max(0, int32(candidate.baseMPByLevel[level-1]))
	if !candidate.mpRules.Known {
		return base, !r.characterBaseInfo.known
	}
	if candidate.mpRules.CommandReductionPermille > 0 {
		// Runtime command mode is not carried by the observed state packets.
		return 0, false
	}
	switch candidate.mpRules.WeaponEffect {
	case 0:
		return base, true
	case 1, 2:
		r.initializeCharacterResourcesUnsafe()
		if !r.characterResources.equipmentComplete || !r.characterResources.weaponTypeKnown {
			return 0, false
		}
		rate, known := shared.CharacterWeaponMPRate(r.partySkillJob, r.characterResources.weaponType, candidate.mpRules.WeaponEffect)
		if !known {
			return 0, false
		}
		return int32(float32(base) * rate), true
	default:
		return 0, false
	}
}
