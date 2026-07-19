package scheduler

import (
	"testing"
	"time"

	"robot/internal/shared"
)

func TestSessionReloginWaitsForSameUID(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = 40 * time.Millisecond
	m.markSessionLogout(17000001, time.Now())

	started := time.Now()
	m.waitSessionRelogin([]shared.RuntimeOnlineUser{{UID: 17000001}})
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("same uid relogin waited %s, want at least 30ms", elapsed)
	}
}

func TestSessionReloginDoesNotDelayOtherUID(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = time.Second
	m.markSessionLogout(17000001, time.Now())

	started := time.Now()
	m.waitSessionRelogin([]shared.RuntimeOnlineUser{{UID: 17000002}})
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("unrelated uid relogin waited %s", elapsed)
	}
}
