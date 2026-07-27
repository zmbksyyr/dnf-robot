package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"robot/internal/shared"
)

type offlineSessionRepository struct {
	missingSchemaRepository
	calls int
}

func (r *offlineSessionRepository) AccountOnline(int) (bool, error) {
	r.calls++
	return false, nil
}

func (*offlineSessionRepository) Stats() sql.DBStats { return sql.DBStats{} }

func (*offlineSessionRepository) PingContext(context.Context) error { return nil }

func (*offlineSessionRepository) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}

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

func TestSessionReleaseRemainingUsesLogoutSafetyWindow(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = 40 * time.Millisecond
	m.markSessionLogout(17000001, time.Now().Add(-sessionWriteSafetyMargin))

	if remaining := m.sessionReleaseRemaining(17000001); remaining <= 0 {
		t.Fatalf("remaining=%s, want active safety window", remaining)
	}
	time.Sleep(45 * time.Millisecond)
	if remaining := m.sessionReleaseRemaining(17000001); remaining != 0 {
		t.Fatalf("remaining=%s, want released session", remaining)
	}
}

func TestClosedSessionInvalidationClearsLogoutSafetyWindow(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = time.Second
	m.markSessionLogout(17000001, time.Now())
	m.characterCacheInvalidate = func(uid int) error {
		if uid != 17000001 {
			t.Fatalf("invalidate uid=%d", uid)
		}
		return nil
	}

	if err := m.invalidateClosedCharacterCache(17000001); err != nil {
		t.Fatal(err)
	}
	if remaining := m.sessionReleaseRemaining(17000001); remaining != 0 {
		t.Fatalf("remaining=%s, want cleared safety window", remaining)
	}
}

func TestClosedSessionInvalidationFailureKeepsLogoutSafetyWindow(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = time.Second
	m.markSessionLogout(17000001, time.Now())
	want := errors.New("game udp unavailable")
	m.characterCacheInvalidate = func(int) error { return want }

	if err := m.invalidateClosedCharacterCache(17000001); !errors.Is(err, want) {
		t.Fatalf("invalidate error=%v, want %v", err, want)
	}
	if remaining := m.sessionReleaseRemaining(17000001); remaining <= 0 {
		t.Fatalf("remaining=%s, want preserved safety window", remaining)
	}
}

func TestWaitAccountOfflineCompletesDelayedNoCacheBoundary(t *testing.T) {
	repo := &offlineSessionRepository{}
	m := testRobotManagerWithConfig(t, "")
	m.database = repo
	m.sessionReloginDelay = 10 * time.Second
	m.markSessionLogout(17000001, time.Now())
	invalidations := 0
	m.characterCacheInvalidate = func(uid int) error {
		if uid != 17000001 {
			t.Fatalf("invalidate uid=%d", uid)
		}
		invalidations++
		return nil
	}

	started := time.Now()
	cancelled, err := m.waitAccountOffline(17000001, nil)
	if err != nil || cancelled {
		t.Fatalf("waitAccountOffline cancelled=%v err=%v", cancelled, err)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("offline boundary took %s, want NoCache fast path", elapsed)
	}
	if invalidations != 1 || repo.calls < 2 {
		t.Fatalf("invalidations=%d account checks=%d, want one invalidation and stable offline confirmation", invalidations, repo.calls)
	}
	if remaining := m.sessionReleaseRemaining(17000001); remaining != 0 {
		t.Fatalf("remaining=%s, want cleared safety window", remaining)
	}
}

func TestWaitAccountOfflineDoesNotRepeatCompletedNoCacheBoundary(t *testing.T) {
	repo := &offlineSessionRepository{}
	m := testRobotManagerWithConfig(t, "")
	m.database = repo
	m.sessionReloginDelay = 10 * time.Second
	m.markSessionLogout(17000001, time.Now())
	invalidations := 0
	m.characterCacheInvalidate = func(uid int) error {
		if uid != 17000001 {
			t.Fatalf("invalidate uid=%d", uid)
		}
		invalidations++
		return nil
	}
	if err := m.invalidateClosedCharacterCache(17000001); err != nil {
		t.Fatal(err)
	}

	cancelled, err := m.waitAccountOffline(17000001, nil)
	if err != nil || cancelled {
		t.Fatalf("waitAccountOffline cancelled=%v err=%v", cancelled, err)
	}
	if invalidations != 1 || repo.calls < 2 {
		t.Fatalf("invalidations=%d account checks=%d, want no duplicate invalidation and stable offline confirmation", invalidations, repo.calls)
	}
}
