package scheduler

import (
	"testing"
	"time"
)

func TestSystemAnnouncementMessage(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 34, 56, 0, time.Local)
	if got := SystemAnnouncementMessageAt(now, 123, 456); got != "12:34:56 在线人数123；拍卖行456类" {
		t.Fatalf("message=%q", got)
	}
	if got := SystemAnnouncementMessageAt(now, -1, -2); got != "12:34:56 在线人数0；拍卖行0类" {
		t.Fatalf("negative message=%q", got)
	}
}
