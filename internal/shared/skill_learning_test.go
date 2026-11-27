package shared

import "testing"

func TestMaxLearnedSkillsUsesJobAndGrowCaps(t *testing.T) {
	SetSkillLearningDefinitions([]SkillLearningDefinition{
		{Job: 8, SkillIndex: 1, Maximum: 50, GrowMaximum: []int{10, 10, 10}},
		{Job: 8, SkillIndex: 26, Maximum: 70, GrowMaximum: []int{50, 50, 50}},
		{Job: 8, SkillIndex: 47, Maximum: 20, GrowMaximum: []int{0, 20, 0}, GrowTypes: []int{1}},
		{Job: 0, SkillIndex: 46, Maximum: 50},
	})
	t.Cleanup(func() { SetSkillLearningDefinitions(nil) })
	got := MaxLearnedSkillsForJob(8, 0x11)
	if len(got) != 3 || got[0] != (LearnedSkill{1, 10}) || got[1] != (LearnedSkill{26, 50}) || got[2] != (LearnedSkill{47, 20}) {
		t.Fatalf("awakened class skills = %v", got)
	}
	if got := MaxLearnedSkillsForJob(8, 2); len(got) != 2 {
		t.Fatalf("learned a different class's skill: %v", got)
	}
}
