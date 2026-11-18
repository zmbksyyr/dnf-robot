package shared

import "testing"

func TestGenericAreaAllowedOnlyRestrictsGuildMembersUsingGuildAgitVillage(t *testing.T) {
	tests := []struct {
		guildID int
		village int
		want    bool
	}{
		{guildID: 0, village: GuildAgitVillage, want: true},
		{guildID: -1, village: GuildAgitVillage, want: false},
		{guildID: 2, village: GuildAgitVillage, want: false},
		{guildID: 2, village: 7, want: true},
	}
	for _, test := range tests {
		if got := GenericAreaAllowed(test.guildID, test.village); got != test.want {
			t.Fatalf("GenericAreaAllowed(%d, %d)=%v want=%v", test.guildID, test.village, got, test.want)
		}
	}
}

func TestFilterGenericAreaMapsKeepsGuildAgitForNonMembers(t *testing.T) {
	maps := []MapCatalogItem{{Village: GuildAgitVillage, Area: 2}, {Village: 1, Area: 0}}
	if got := FilterGenericAreaMaps(maps, 0); len(got) != 2 {
		t.Fatalf("non-member maps=%v want both maps", got)
	}
	got := FilterGenericAreaMaps(maps, 2)
	if len(got) != 1 || got[0].Village != 1 {
		t.Fatalf("guild-member maps=%v want only village 1", got)
	}
}

func TestStoreAreaPolicyKeepsMovementAndGuildRules(t *testing.T) {
	for _, guildID := range []int{0, -1, 2} {
		if StoreAreaAllowed(guildID, 7) {
			t.Fatalf("store allowed in village 7 for guild %d", guildID)
		}
		for _, village := range []int{1, 6, GuildAgitVillage, 9} {
			if got, want := StoreAreaAllowed(guildID, village), GenericAreaAllowed(guildID, village); got != want {
				t.Fatalf("guild %d village %d: store=%v movement=%v", guildID, village, got, want)
			}
		}
		if !GenericAreaAllowed(guildID, 7) {
			t.Fatalf("ordinary movement to village 7 changed for guild %d", guildID)
		}
	}
}
