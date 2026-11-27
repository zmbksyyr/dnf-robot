package pvf

import (
	"encoding/json"
	"os"

	"robot/internal/shared"
)

const pvfSkillLearningExportName = "skill_learning_catalog.json"

type skillLearningCatalog struct {
	Skills []shared.SkillLearningDefinition `json:"skills"`
}

func loadSkillLearningCatalog(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var catalog skillLearningCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return err
	}
	shared.SetSkillLearningDefinitions(catalog.Skills)
	return nil
}

func extractSkillLearningDefinitions(archive *pvfArchive) []shared.SkillLearningDefinition {
	entries := make([]shared.SkillLearningDefinition, 0)
	for job, list := range skillJobLists {
		for _, skill := range parsePVFList(archive.text("skill/" + list)) {
			if skill.ID <= 0 || skill.ID > 255 {
				continue
			}
			text := archive.text("skill/" + skill.Path)
			maximum := pvfTagInts(text, "maximum level")
			if len(maximum) != 1 || maximum[0] <= 0 {
				continue
			}
			entries = append(entries, shared.SkillLearningDefinition{
				Job: job, SkillIndex: skill.ID, Maximum: maximum[0],
				GrowMaximum: pvfTagInts(text, "growtype maximum level"),
				GrowTypes:   pvfTagInts(text, "skill fitness growtype"),
			})
		}
	}
	return entries
}
