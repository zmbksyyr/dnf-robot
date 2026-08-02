package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyCatalogDirectoryNeverReadsWorkingDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	files := map[string]string{
		"robot_shout_templates.json": `{"channel":"local","type":3,"messages":["cwd-shout"]}`,
		"robot_name_templates.json":  `{"names":["cwd-name"]}`,
		"map_catalog.json":           `[{"village":1,"area":0}]`,
		"equipment_catalog.json":     `[{"id":123}]`,
		"stackable_catalog.json":     `[{"id":456}]`,
		"party_skill_catalog.json":   `{"enabled":true,"max_skill_level":70,"skills":[{"job":1,"skill_index":2,"state":3,"level":1}]}`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}

	shouts := ShoutTemplates("")
	if len(shouts.Messages) != 1 || shouts.Messages[0] != "hello" {
		t.Fatalf("empty directory read shout template from cwd: %+v", shouts)
	}
	names := NameTemplates("")
	if len(names.Names) != 0 || len(names.Prefixes) == 0 {
		t.Fatalf("empty directory read name template from cwd: %+v", names)
	}
	if maps := ViewMaps(""); len(maps) != 0 {
		t.Fatalf("empty directory read map catalog from cwd: %+v", maps)
	}
	if items := ViewItemCatalogs(""); len(items.Equipment) != 0 || len(items.Stackable) != 0 {
		t.Fatalf("empty directory read item catalogs from cwd: %+v", items)
	}
	if stackable := ViewStackable(""); len(stackable) != 0 {
		t.Fatalf("empty directory read stackable catalog from cwd: %+v", stackable)
	}
	if err := LoadPartySkills(""); err == nil {
		t.Fatal("empty directory loaded party skills from cwd")
	}
}
