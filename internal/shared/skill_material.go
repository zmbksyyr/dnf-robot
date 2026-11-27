package shared

import "slices"

// SkillMaterialDefinition preserves the native definition, including empty
// blocks and their original order. It does not resolve mode inheritance,
// learned level, active equipment, owner or dynamic modifiers.
type SkillMaterialDefinition struct {
	Known        bool                 `json:"known"`
	MaximumLevel int32                `json:"maximum_level,omitempty"`
	Blocks       []SkillMaterialBlock `json:"blocks,omitempty"`
}

type SkillMaterialBlock struct {
	Mode string             `json:"mode"` // common, dungeon, pvp, death tower, warroom
	Rows []SkillMaterialRow `json:"rows"`
}

type SkillMaterialRow struct {
	ItemID int32 `json:"item_id"`
	Start  int32 `json:"start"`
	End    int32 `json:"end"`
}

func (d SkillMaterialDefinition) Clone() SkillMaterialDefinition {
	d.Blocks = slices.Clone(d.Blocks)
	for i := range d.Blocks {
		d.Blocks[i].Rows = slices.Clone(d.Blocks[i].Rows)
	}
	return d
}

func (d SkillMaterialDefinition) Equal(other SkillMaterialDefinition) bool {
	return d.Known == other.Known && d.MaximumLevel == other.MaximumLevel &&
		slices.EqualFunc(d.Blocks, other.Blocks, func(a, b SkillMaterialBlock) bool {
			return a.Mode == b.Mode && slices.Equal(a.Rows, b.Rows)
		})
}

func commonSkillMaterialDefinition(a, b SkillMaterialDefinition) SkillMaterialDefinition {
	if !a.Equal(b) {
		return SkillMaterialDefinition{}
	}
	return a
}
