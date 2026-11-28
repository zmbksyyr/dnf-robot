package shared

import (
	"fmt"
	"math"
	"sync/atomic"
)

// ResourceEffect preserves the native titlebook stat/mode/value tuple.
type ResourceEffect struct {
	Stat    int     `json:"stat"`
	Percent bool    `json:"percent"`
	Value   float32 `json:"value"`
}

type CharacterWeaponResources struct {
	Job       int          `json:"job"`
	SkillInfo [][4]float32 `json:"skill_info"`
}

type CharacterResourceDefinitions struct {
	Version               int                        `json:"version"`
	TitlebookDefaultKnown bool                       `json:"titlebook_default_known"`
	TitlebookDefault      []ResourceEffect           `json:"titlebook_default"`
	Weapons               []CharacterWeaponResources `json:"weapons"`
}

// ResourceBonus contains only HP/MP and the primary attributes that feed them.
// Percentage fields are additive terms, not complete final multipliers.
type ResourceBonus struct {
	HP, MP, Vitality, Spirit                 int32
	HPRate, MPRate, VitalityRate, SpiritRate float32
}

type characterResourceDefinitionsSnapshot struct {
	defaultKnown bool
	defaultBonus ResourceBonus
	weapons      map[int][][4]float32
}

var characterResourceDefinitions atomic.Pointer[characterResourceDefinitionsSnapshot]
var characterResourceGeneration atomic.Uint64

func CharacterResourceGeneration() uint64 { return characterResourceGeneration.Load() }

func SetCharacterResourceDefinitions(definitions *CharacterResourceDefinitions) error {
	if definitions == nil {
		characterResourceDefinitions.Store(nil)
		characterResourceGeneration.Add(1)
		return nil
	}
	if definitions.Version != 1 {
		return fmt.Errorf("unsupported character resource catalog version %d", definitions.Version)
	}
	snapshot := &characterResourceDefinitionsSnapshot{defaultKnown: definitions.TitlebookDefaultKnown, weapons: make(map[int][][4]float32)}
	for _, effect := range definitions.TitlebookDefault {
		if effect.Stat < 0 || effect.Stat > 63 || math.IsNaN(float64(effect.Value)) || math.IsInf(float64(effect.Value), 0) {
			return fmt.Errorf("invalid titlebook resource effect %+v", effect)
		}
		var flat *int32
		var rate *float32
		switch effect.Stat {
		case 13:
			flat, rate = &snapshot.defaultBonus.HP, &snapshot.defaultBonus.HPRate
		case 48:
			flat, rate = &snapshot.defaultBonus.MP, &snapshot.defaultBonus.MPRate
		case 3:
			flat, rate = &snapshot.defaultBonus.Vitality, &snapshot.defaultBonus.VitalityRate
		case 2:
			flat, rate = &snapshot.defaultBonus.Spirit, &snapshot.defaultBonus.SpiritRate
		default:
			continue
		}
		if effect.Percent {
			*rate += float32(effect.Value * float32(0.01))
		} else {
			*flat += int32(effect.Value)
		}
	}
	for _, weapon := range definitions.Weapons {
		if weapon.Job < 0 || weapon.Job > 10 || len(weapon.SkillInfo) == 0 || len(weapon.SkillInfo) > 256 {
			return fmt.Errorf("invalid character weapon table job=%d rows=%d", weapon.Job, len(weapon.SkillInfo))
		}
		if _, duplicate := snapshot.weapons[weapon.Job]; duplicate {
			return fmt.Errorf("duplicate character weapon table job=%d", weapon.Job)
		}
		for _, row := range weapon.SkillInfo {
			for _, value := range row {
				if value < 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					return fmt.Errorf("invalid character weapon multiplier job=%d", weapon.Job)
				}
			}
		}
		snapshot.weapons[weapon.Job] = append([][4]float32(nil), weapon.SkillInfo...)
	}
	characterResourceDefinitions.Store(snapshot)
	characterResourceGeneration.Add(1)
	return nil
}

func TitlebookDefaultResourceBonus() (ResourceBonus, bool) {
	snapshot := characterResourceDefinitions.Load()
	if snapshot == nil || !snapshot.defaultKnown {
		return ResourceBonus{}, false
	}
	return snapshot.defaultBonus, true
}

func CharacterWeaponMPRate(job, subtype int, effect byte) (float32, bool) {
	snapshot := characterResourceDefinitions.Load()
	if snapshot == nil || subtype < 0 || effect < 1 || effect > 2 {
		return 0, false
	}
	rows := snapshot.weapons[job]
	if subtype >= len(rows) {
		return 0, false
	}
	// Native weapon-effect arguments 0/2 select physical/magical MP columns.
	return rows[subtype][int(effect-1)*2], true
}
