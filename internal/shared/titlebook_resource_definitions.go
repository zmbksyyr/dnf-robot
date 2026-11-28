package shared

import (
	"fmt"
	"slices"
)

// TitlebookElementResources preserves vacant (-1) and special (1) slots, as
// well as native -1 achievement/title sentinels. These are definitions only.
type TitlebookElementResources struct {
	Type        int32   `json:"type"`
	Achievement int32   `json:"achievement"`
	Titles      []int32 `json:"titles"`
}

type TitlebookSectionResources struct {
	Start     int              `json:"start"`
	End       int              `json:"end"`
	FrameType int32            `json:"frame_type"`
	Effects   []ResourceEffect `json:"effects"`
}

type TitlebookCategoryResources struct {
	Category int                         `json:"category"`
	Elements []TitlebookElementResources `json:"elements"`
	Sections []TitlebookSectionResources `json:"sections"`
}

func cloneTitlebookCategory(category TitlebookCategoryResources) TitlebookCategoryResources {
	category.Elements = slices.Clone(category.Elements)
	for i := range category.Elements {
		category.Elements[i].Titles = slices.Clone(category.Elements[i].Titles)
	}
	category.Sections = slices.Clone(category.Sections)
	for i := range category.Sections {
		category.Sections[i].Effects = slices.Clone(category.Sections[i].Effects)
	}
	return category
}

func (snapshot *characterResourceDefinitionsSnapshot) setTitlebookCollection(definitions *CharacterResourceDefinitions) error {
	if !definitions.TitlebookCollectionKnown {
		if len(definitions.TitlebookCollection) != 0 {
			return fmt.Errorf("unknown titlebook collection contains categories")
		}
		return nil
	}
	if len(definitions.TitlebookCollection) != len(snapshot.collection) {
		return fmt.Errorf("incomplete titlebook collection categories")
	}
	seen := [4]bool{}
	for _, category := range definitions.TitlebookCollection {
		if category.Category < 0 || category.Category >= len(seen) || seen[category.Category] {
			return fmt.Errorf("invalid or duplicate titlebook category %d", category.Category)
		}
		seen[category.Category] = true
		if len(category.Elements) == 0 || len(category.Elements) > 200 || len(category.Sections) == 0 || len(category.Sections)*10 != len(category.Elements) {
			return fmt.Errorf("invalid titlebook dimensions category=%d", category.Category)
		}
		for _, element := range category.Elements {
			if element.Type < -1 || element.Type > 1 || element.Achievement < -1 || len(element.Titles) > 200 || element.Type == -1 && (element.Achievement != -1 || len(element.Titles) != 0) {
				return fmt.Errorf("invalid titlebook element category=%d", category.Category)
			}
			for _, title := range element.Titles {
				if title < -1 {
					return fmt.Errorf("invalid titlebook title category=%d", category.Category)
				}
			}
		}
		for ordinal, section := range category.Sections {
			start, end := ordinal*10, (ordinal+1)*10
			for slot := start; slot < end; slot++ {
				if category.Elements[slot].Type == -1 {
					end = slot
					break
				}
			}
			if section.Start != start || section.End != end {
				return fmt.Errorf("invalid titlebook section range category=%d section=%d", category.Category, ordinal)
			}
			for _, effect := range section.Effects {
				if !validResourceEffect(effect) {
					return fmt.Errorf("invalid titlebook section effect category=%d section=%d", category.Category, ordinal)
				}
			}
		}
		snapshot.collection[category.Category] = cloneTitlebookCategory(category)
	}
	snapshot.collectionKnown = true
	return nil
}

// TitlebookCollectionDefinition returns an owned definition, not a completion
// verdict or an applied bonus. Missing definitions remain distinct from empty
// sections and sections whose rewards have no attribute effects.
func TitlebookCollectionDefinition(category int) (TitlebookCategoryResources, bool) {
	snapshot := characterResourceDefinitions.Load()
	if snapshot == nil || !snapshot.collectionKnown || category < 0 || category >= len(snapshot.collection) {
		return TitlebookCategoryResources{}, false
	}
	return cloneTitlebookCategory(snapshot.collection[category]), true
}
