package dnf

import (
	"encoding/binary"
	"fmt"

	foundationlog "robot/internal/foundation/log"
	"robot/internal/shared"
)

// Login pool uses nominal PVF stats. Instance quality, enchantments and passive
// modifiers are not yet included; observed resource packets replace this value.
type characterResourcePool struct {
	known  bool
	hp, mp int32
}

func (r *RobotVo) initializeCharacterResourcesUnsafe() {
	info := r.characterBaseInfo
	if r.characterResources.known || !info.known || !info.equipment.known || !r.characterLevel.known || !r.partySkillLoaded {
		return
	}
	hp := int(binary.LittleEndian.Uint32(info.stats[0:4]) / 10)
	mp := int(info.baseMP())
	vitality := int(binary.LittleEndian.Uint16(info.stats[0xa:0xc]) / 10)
	spirit := int(binary.LittleEndian.Uint16(info.stats[0xe:0x10]) / 10)
	hpRate, mpRate := float32(1), float32(1)
	equipped := make(map[int]bool)
	var sets []shared.EquipmentResourceSet
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
		item, ok := shared.ResourceEquipment(id)
		if !ok || item.Resources == nil || item.Level > int(r.characterLevel.level) {
			continue
		}
		if len(item.UseJob) > 0 {
			usable := false
			for _, job := range item.UseJob {
				if job == r.partySkillJob {
					usable = true
					break
				}
			}
			if !usable {
				continue
			}
		}
		if item.Durability > 0 {
			if endurance, known := info.equipment.nativeEndurance(slot); known && endurance == 0 {
				continue
			}
		}
		equipped[id] = true
		res := item.Resources
		add(shared.ResourceStats{HP: res.HP, MP: res.MP, Vitality: res.Vitality, Spirit: res.Spirit, HPRate: res.HPRate, MPRate: res.MPRate})
		if slot < 10 && len(record) > 9 {
			option := int(record[9]&31) - 1
			if option >= 0 && option < len(res.Options) {
				add(res.Options[option])
			}
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
	hpRate += float32(vitality) / 250
	mpRate += float32(spirit) / 250
	r.characterResources = characterResourcePool{known: true, hp: int32(float32(hp) * hpRate), mp: int32(float32(mp) * mpRate)}
	foundationlog.Robotf("[CHARACTER_RESOURCE_INIT] uid=%d cid=%d source=nominal_pvf hp=%d mp=%d vitality=%d spirit=%d\n", r.UID, r.CID, r.characterResources.hp, r.characterResources.mp, vitality, spirit)
}

func (r *RobotVo) seedPartyResourcesUnsafe() {
	r.initializeCharacterResourcesUnsafe()
	if !r.characterResources.known || r.partySelfPeer.uniqueID == 0 {
		return
	}
	if r.partyDungeonWorld.actors == nil {
		r.partyDungeonWorld.actors = make(map[[4]byte]partyDungeonActor)
	}
	ref := partyDungeonPlayerReference(r.partySelfPeer.uniqueID)
	actor := r.partyDungeonWorld.actors[ref]
	if !actor.hpKnown {
		actor.hp, actor.hpKnown = r.characterResources.hp, true
	}
	if !actor.mpKnown {
		actor.mp, actor.mpKnown = r.characterResources.mp, true
	}
	r.partyDungeonWorld.actors[ref] = actor
}

func (r *RobotVo) rememberPartyResourcesUnsafe() {
	actor := r.partyDungeonWorld.actors[partyDungeonPlayerReference(r.partySelfPeer.uniqueID)]
	if actor.hpKnown {
		r.characterResources.hp = actor.hp
	}
	if actor.mpKnown {
		r.characterResources.mp = actor.mp
	}
}
