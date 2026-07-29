package webadmin

import "testing"

func TestTruncateServerScriptOutputKeepsTail(t *testing.T) {
	if got := truncateServerScriptOutput("abcdefgh", 4); got != "efgh" {
		t.Fatalf("output=%q", got)
	}
	if got := truncateServerScriptOutput("abc", 4); got != "abc" {
		t.Fatalf("short output=%q", got)
	}
}
