package pvf

import (
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"

	"robot/internal/shared"
)

func TestSkillMaterialDefinitionTypedRowsModesAndOrder(t *testing.T) {
	text := "[maximum level] 70 [consume item] 3037 1 2 3037 -3 4 [/consume item] " +
		"[pvp] [consume item] [/consume item] [/pvp] " +
		"[death tower] [consume item] 0 55 200 [/consume item] [/death tower] " +
		"[consume item] 4183 0 0 [/consume item]"
	want := shared.SkillMaterialDefinition{Known: true, MaximumLevel: 70, Blocks: []shared.SkillMaterialBlock{
		{Mode: "common", Rows: []shared.SkillMaterialRow{{ItemID: 3037, Start: 1, End: 2}, {ItemID: 3037, Start: -3, End: 4}}},
		{Mode: "pvp", Rows: []shared.SkillMaterialRow{}},
		{Mode: "death tower", Rows: []shared.SkillMaterialRow{{ItemID: 0, Start: 55, End: 200}}},
		{Mode: "common", Rows: []shared.SkillMaterialRow{{ItemID: 4183, Start: 0, End: 0}}},
	}}
	archive := &pvfArchive{}
	tokens, valid := archive.definitionTokens([]byte(text))
	if got := skillMaterialDefinitionFromTokens(tokens, valid); !reflect.DeepEqual(got, want) {
		t.Fatalf("raw material definitions changed: %+v", got)
	}
	// Encode exactly the same typed tokens into PVF binary. Tag-like string
	// literals remain literals and do not create additional material blocks.
	archive.stringList = nil
	data := []byte{0xb0, 0xd0}
	for _, token := range tokens {
		value := token.value
		if token.kind == 5 || token.kind == 7 {
			value = int32(len(archive.stringList))
			archive.stringList = append(archive.stringList, token.spelling)
		}
		row := make([]byte, 5)
		row[0] = token.kind
		binary.LittleEndian.PutUint32(row[1:], uint32(value))
		data = append(data, row...)
	}
	tokens, valid = archive.definitionTokens(data)
	if got := skillMaterialDefinitionFromTokens(tokens, valid); !reflect.DeepEqual(got, want) {
		t.Fatalf("binary material definitions differ: %+v", got)
	}
	if got := skillMaterialDefinitionFromTokens(tokens, false); got.Known || len(got.Blocks) != 0 {
		t.Fatal("truncated binary retained partial material data")
	}
}

func TestSkillMaterialDefinitionRejectsIncompleteOrUnresolvedScope(t *testing.T) {
	for _, suffix := range []string{
		"[consume item] 3037 1 [/consume item]",
		"[consume item] 3037 1 1.0 [/consume item]",
		"[consume item] `3037` 1 1 [/consume item]",
		"[consume item] 3037 1 1",
		"[/consume item]",
		"[Consume Item] [/consume item]",
		"[consume item] [/Consume Item]",
		"[if] [consume item] 3037 1 1 [/consume item] [/if]",
		"[pvp] [dungeon] [consume item] 3037 1 1 [/consume item] [/dungeon] [/pvp]",
		"[dungeon] [consume item] 3037 1 1 [/consume item]",
		"[maximum level] 10",
		"[unknown] 1 [/other]",
	} {
		t.Run(suffix, func(t *testing.T) {
			tokens, valid := textDefinitionTokens("[maximum level] 70 " + suffix)
			if got := skillMaterialDefinitionFromTokens(tokens, valid); got.Known || len(got.Blocks) != 0 {
				t.Fatalf("unresolved material definition accepted: %+v", got)
			}
		})
	}
	for _, text := range []string{"", "[type] `active`", "[maximum level] 0", "[maximum level] -1", "[maximum level] 1.0", "[maximum level] 3 4"} {
		tokens, valid := textDefinitionTokens(text)
		if got := skillMaterialDefinitionFromTokens(tokens, valid); got.Known {
			t.Fatal("invalid maximum supplied a complete material definition", text)
		}
	}
	tokens, valid := textDefinitionTokens("[maximum level] 70 [name] `[consume item]`")
	if got := skillMaterialDefinitionFromTokens(tokens, valid); !got.Known || len(got.Blocks) != 0 {
		t.Fatal("tag-like literal became a material block", got)
	}
}

func TestSkillMaterialDefinitionsReachActualExportAndConflictsStayUnknown(t *testing.T) {
	for _, list := range []string{"1 `atmage/a.skl`", "1 `atmage/a.skl` 1 `atmage/b.skl`", "1 `atmage/b.skl` 1 `atmage/a.skl`"} {
		a := &pvfArchive{files: map[string]*pvfFile{
			"sqr/example_load_state.nut": {Data: []byte(`IRDSQRCharacter.pushState(8,"example.nut","example",20,1);`)},
			"sqr/example.nut":            {Data: []byte("function onSetState_example(obj,state,data,reset) {}")},
			"skill/atmageskill.lst":      {Data: []byte(list)},
			"skill/atmage/a.skl":         {Data: []byte("[maximum level] 70 [consume item] 3037 1 2 [/consume item]")},
			"skill/atmage/b.skl":         {Data: []byte("[maximum level] 70 [consume item] 3037 1 3 [/consume item]")},
		}}
		entries := extractSkillStateCatalog(a)
		if len(entries) != 1 {
			t.Fatal("missing material skill export", entries)
		}
		data, err := json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		var reloaded []shared.SkillState
		if err = json.Unmarshal(data, &reloaded); err != nil {
			t.Fatal(err)
		}
		d := reloaded[0].Materials
		if reloaded[0].ItemCostFree || d.Known != (list == "1 `atmage/a.skl`") {
			t.Fatal("duplicate identity chose a material interpretation", d)
		}
		if d.Known && (d.MaximumLevel != 70 || len(d.Blocks) != 1 || d.Blocks[0].Rows[0].End != 2) {
			t.Fatal("JSON export lost material rows", d)
		}
	}
}
