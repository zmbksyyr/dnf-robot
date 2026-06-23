package config

import "testing"

func TestINIValueKeepsCommentCharacters(t *testing.T) {
	cfg, err := LoadFromString("[db]\npassword = uu5!^%jg#semi;tail\n# ignored = yes\n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.GetString("db", "password", "")
	want := "uu5!^%jg#semi;tail"
	if got != want {
		t.Fatalf("password mismatch: got %q want %q", got, want)
	}
}
