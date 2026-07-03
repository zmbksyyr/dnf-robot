package robottemplate

import "testing"

func TestSafeShoutMessage(t *testing.T) {
	if got := SafeShoutMessage(" \x01hello "); got != "hello" {
		t.Fatalf("safe shout got %q want hello", got)
	}
	if got := SafeShoutMessage(""); got != "hello" {
		t.Fatalf("empty shout got %q want hello", got)
	}
}

func TestPrepareShoutSeparatesLocalAndWorld(t *testing.T) {
	localType, localChannel, localOut := PrepareShout("hello", false)
	if localType != 3 || localChannel != "local" || localOut != "hello" {
		t.Fatalf("local shout got type=%d channel=%s out=%q", localType, localChannel, localOut)
	}

	worldType, worldChannel, worldOut := PrepareShout("hello", true)
	if worldType != 11 || worldChannel != "world" || worldOut != "hello" {
		t.Fatalf("world shout got type=%d channel=%s out=%q, want type=11 channel=world out=hello", worldType, worldChannel, worldOut)
	}
}

func TestParseStringListJSON(t *testing.T) {
	got := ParseStringListJSON([]byte(`{"messages":[" a ","a","b"]}`))
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("messages got %#v want [a b]", got)
	}
}

func TestRenderName(t *testing.T) {
	tpl := NameTemplates{
		Prefixes:  []string{"Bot"},
		Middles:   []string{"Name"},
		Suffixes:  []string{"X"},
		Pattern:   "{prefix}{middle}{suffix}{uid_tail}",
		NumberMin: 1,
		NumberMax: 9,
	}
	got := RenderName(tpl, 123, 0, nil, nil)
	if got != "BotNameX00123" {
		t.Fatalf("name got %q want BotNameX00123", got)
	}
}

func TestNameEncodingRules(t *testing.T) {
	if !FitsGameSlot("RobotName") {
		t.Fatalf("ascii name should fit game slot")
	}
	if FitsGameSlot("") {
		t.Fatalf("empty name should not fit game slot")
	}
	if got := DBName("RobotName"); got == "" {
		t.Fatalf("db name should not be empty")
	}
}
