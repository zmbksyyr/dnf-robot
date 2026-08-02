package runtime

import (
	"encoding/json"
	"testing"
)

func TestDefaultStoreTitlesContainSixtyFourUniqueNames(t *testing.T) {
	data, err := defaultFiles.ReadFile("defaults/robot_store_titles.json")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	if err := json.Unmarshal(data, &titles); err != nil {
		t.Fatal(err)
	}
	if len(titles) != 64 {
		t.Fatalf("default store titles=%d, want 64", len(titles))
	}
	seen := make(map[string]bool, len(titles))
	for _, title := range titles {
		if title == "" || seen[title] {
			t.Fatalf("invalid or duplicate default store title %q", title)
		}
		seen[title] = true
	}
}
