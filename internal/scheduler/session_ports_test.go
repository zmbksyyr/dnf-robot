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
	calls         int
	online        bool
	checked       chan struct{}
	onlineSamples []bool
	accountErr    error
}

func (r *offlineSessionRepository) AccountOnline(int) (bool, error) {
	r.calls++
	if r.checked != nil {
		select {
		case r.checked <- struct{}{}:
		default:
		}
	}
	if len(r.onlineSamples) > 0 {
		online := r.onlineSamples[0]
		r.onlineSamples = r.onlineSamples[1:]
		return online, r.accountErr
	}
	return r.online, r.accountErr
}

func TestLogoutConfirmationWaitsForServerEvenDuringShutdown(t *testing.T) {
	repo := &offlineSessionRepository{onlineSamples: []bool{true, true, false}}
	m := testRobotManagerWithConfig(t, "")
	m.database = repo
	m.RequestShutdown()
	if err := (sessionActionEnv{manager: m}).WaitAccountLogout(17000001); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 3 {
		t.Fatalf("server confirmation used %d queries, want 3", repo.calls)
	}
}

func TestLogoutConfirmationPreservesDatabaseError(t *testing.T) {
	want := errors.New("account database unavailable")
	repo := &offlineSessionRepository{accountErr: want}
	m := testRobotManagerWithConfig(t, "")
	m.database = repo
	if err := (sessionActionEnv{manager: m}).WaitAccountLogout(17000001); !errors.Is(err, want) {
		t.Fatalf("confirmation error = %v, want %v", err, want)
	}
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
	if err := m.waitSessionRelogin([]shared.RuntimeOnlineUser{{UID: 17000001}}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("same uid relogin waited %s, want at least 30ms", elapsed)
	}
}

func TestSessionReloginDoesNotDelayOtherUID(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = time.Second
	m.markSessionLogout(17000001, time.Now())

	started := time.Now()
	if err := m.waitSessionRelogin([]shared.RuntimeOnlineUser{{UID: 17000002}}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("unrelated uid relogin waited %s", elapsed)
	}
}

func TestShutdownInterruptsPendingSessionRelogin(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.markSessionLogout(17000001, time.Now())
	done := make(chan error, 1)
	go func() { done <- m.waitSessionRelogin([]shared.RuntimeOnlineUser{{UID: 17000001}}) }()
	select {
	case err := <-done:
		t.Fatalf("same uid did not wait: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	m.RequestShutdown()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("relogin error=%v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not interrupt the relogin safety wait")
	}
	// Even an unrelated UID must not start a new session after shutdown.
	if err := m.waitSessionRelogin([]shared.RuntimeOnlineUser{{UID: 17000002}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("new session after shutdown error=%v", err)
	}
}

func TestShutdownAbortsOfflineCleanupWithoutGrantingWriteBoundary(t *testing.T) {
	repo := &offlineSessionRepository{online: true, checked: make(chan struct{}, 1)}
	m := testRobotManagerWithConfig(t, "")
	m.database = repo
	m.markSessionLogout(17000001, time.Now())
	done := make(chan error, 1)
	go func() {
		_, err := m.waitAccountOffline(17000001, nil)
		done <- err
	}()
	select {
	case <-repo.checked:
	case <-time.After(time.Second):
		t.Fatal("offline cleanup did not start")
	}
	m.RequestShutdown()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cleanup error=%v, want cancellation rather than permission to write", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not interrupt cleanup with a nil callback")
	}
	if remaining := m.sessionReleaseRemaining(17000001); remaining <= 0 {
		t.Fatal("cancellation cleared the account safety boundary")
	}
	if _, err := m.offlineCharacterForWrite(17000001, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("new write workflow after shutdown error=%v", err)
	}
	if _, err := m.waitCharacterRunning(17000001, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("running-state wait after shutdown error=%v", err)
	}
}

func TestGateAreaForVillageUsesCurrentPVFCatalog(t *testing.T) {
	maps := []shared.MapCatalogItem{
		{Village: 2, Area: 9, Use: true},
		{Village: 2, Area: 7, Use: true, Gate: true},
		{Village: 3, Area: 4, Use: false, Gate: true},
	}
	if area, ok := gateAreaForVillage(maps, 2); !ok || area != 7 {
		t.Fatalf("gate area=%d ok=%t, want current PVF area 7", area, ok)
	}
	if _, ok := gateAreaForVillage(maps, 3); ok {
		t.Fatal("unusable gate area was accepted")
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

func TestMarkSessionLogoutPrunesExpiredEntries(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = time.Second
	m.sessionLastLogout[17000001] = time.Now().Add(-time.Hour)
	m.markSessionLogout(17000002, time.Now())

	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if _, ok := m.sessionLastLogout[17000001]; ok {
		t.Fatal("expired logout entry was not pruned")
	}
	if _, ok := m.sessionLastLogout[17000002]; !ok {
		t.Fatal("current logout entry was not retained")
	}
}

func TestMarkSessionLogoutRateLimitsExpiredEntryScans(t *testing.T) {
	m := testRobotManagerWithConfig(t, "")
	m.sessionReloginDelay = time.Second
	now := time.Now()
	m.sessionLastLogout[17000001] = now.Add(-time.Hour)
	m.sessionLogoutCleanupAt = now.Add(time.Minute)

	m.markSessionLogout(17000002, now)
	if _, ok := m.sessionLastLogout[17000001]; !ok {
		t.Fatal("rate-limited mark unexpectedly scanned the logout table")
	}

	m.sessionLogoutCleanupAt = time.Time{}
	m.markSessionLogout(17000003, now)
	if _, ok := m.sessionLastLogout[17000001]; ok {
		t.Fatal("due cleanup did not prune the expired logout entry")
	}
	for _, uid := range []int{17000002, 17000003} {
		if _, ok := m.sessionLastLogout[uid]; !ok {
			t.Fatalf("current logout uid=%d was not retained", uid)
		}
	}
}

func BenchmarkMarkSessionLogoutRateLimited(b *testing.B) {
	now := time.Now()
	m := &RobotManager{
		sessionReloginDelay:    15 * time.Second,
		sessionLastLogout:      make(map[int]time.Time, 10_256),
		sessionLogoutCleanupAt: now.Add(time.Hour),
	}
	for uid := 1; uid <= 10_000; uid++ {
		m.sessionLastLogout[uid] = now.Add(-time.Hour)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.markSessionLogout(20_000+(i&255), now)
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
