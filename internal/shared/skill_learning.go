package shared

import "sync/atomic"

type SkillLearningDefinition struct {
	Job         int   `json:"job"`
	SkillIndex  int   `json:"skill_index"`
	Maximum     int   `json:"maximum"`
	GrowMaximum []int `json:"grow_maximum,omitempty"`
	GrowTypes   []int `json:"grow_types,omitempty"`
}

type LearnedSkill struct {
	Index, Level byte
}

var skillLearningSnapshot atomic.Pointer[[]SkillLearningDefinition]

func SetSkillLearningDefinitions(entries []SkillLearningDefinition) {
	copyOf := append([]SkillLearningDefinition(nil), entries...)
	for i := range copyOf {
		copyOf[i].GrowMaximum = append([]int(nil), copyOf[i].GrowMaximum...)
		copyOf[i].GrowTypes = append([]int(nil), copyOf[i].GrowTypes...)
	}
	skillLearningSnapshot.Store(&copyOf)
}

// Awakening occupies the high nibble; the low nibble identifies the class.
// Skill levels use that class's PVF cap, rather than a blanket level of 70.
func MaxLearnedSkillsForJob(job, grow int) []LearnedSkill {
	definitions := skillLearningSnapshot.Load()
	if definitions == nil {
		return nil
	}
	grow &= 15
	var levels [256]byte
	for _, entry := range *definitions {
		if entry.Job != job || entry.SkillIndex <= 0 || entry.SkillIndex > 255 {
			continue
		}
		if len(entry.GrowTypes) > 0 {
			allowed := false
			for _, class := range entry.GrowTypes {
				allowed = allowed || class == grow
			}
			if !allowed {
				continue
			}
		}
		level := entry.Maximum
		if len(entry.GrowMaximum) > 0 {
			if grow >= len(entry.GrowMaximum) {
				continue
			}
			level = min(level, entry.GrowMaximum[grow])
		}
		if level > 0 {
			levels[entry.SkillIndex] = byte(min(level, 255))
		}
	}
	var learned []LearnedSkill
	for index, level := range levels {
		if level > 0 {
			learned = append(learned, LearnedSkill{byte(index), level})
		}
	}
	return learned
}
