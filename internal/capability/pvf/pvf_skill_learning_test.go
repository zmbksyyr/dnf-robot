package pvf

import (
	"testing"

	"robot/internal/shared"
)

func TestLearningCatalogPreservesUnpromotedGrowZero(t *testing.T) {
	a := &pvfArchive{files: map[string]*pvfFile{
		"skill/atmageskill.lst":        {Data: []byte("1\t`atmage/windstrike.skl`\n26\t`atmage/watercannon.skl`")},
		"skill/atmage/windstrike.skl":  {Data: []byte("[maximum level]\n50\n[growtype maximum level]\n10 10 10\n[skill fitness growtype]\n0 1 2\n[/skill fitness growtype]")},
		"skill/atmage/watercannon.skl": {Data: []byte("[maximum level]\n70\n[growtype maximum level]\n50 50 50\n[skill fitness growtype]\n0 1 2\n[/skill fitness growtype]")},
	}}
	shared.SetSkillLearningDefinitions(extractSkillLearningDefinitions(a))
	t.Cleanup(func() { shared.SetSkillLearningDefinitions(nil) })
	got := shared.MaxLearnedSkillsForJob(8, 0)
	if len(got) != 2 || got[0].Level != 10 || got[1].Level != 50 {
		t.Fatalf("base-class maximum skills = %v", got)
	}
}
