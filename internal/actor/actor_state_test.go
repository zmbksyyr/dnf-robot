package actor

import (
	"strings"
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

type rejectedLoginRuntime struct {
	manualAttachRuntime
	logoutOK bool
	logouts  int
}

func (r *rejectedLoginRuntime) Logout(uid int) robotcap.ActionResult {
	r.logouts++
	if !r.logoutOK {
		return robotcap.ActionResult{UID: uid, State: robotcap.ActionStateFailed}
	}
	r.status = robotcap.RuntimeStatus{}
	return robotcap.ActionResult{UID: uid, OK: true, State: robotcap.ActionStateClosed}
}

func TestRejectedLoginPausesWithoutRecyclingOrDeletingCharacter(t *testing.T) {
	runtime := &rejectedLoginRuntime{}
	runtime.status = robotcap.RuntimeStatus{UID: 101, CID: 201, StateName: robotcap.RuntimeStateWrong, DisconnectReason: 36}
	a := NewActor(1, ModeAuto, runtime)
	a.resetForUID(101)
	a.setLastOnlineTry(time.Now().Add(-time.Second))
	for i := 0; i < 5; i++ {
		a.tick(time.Now().Add(time.Duration(i) * time.Minute))
	}
	if snap := a.snapshot(); snap.UID != 101 || snap.OnlineDesired || snap.Failures != 1 || runtime.logins != 0 || runtime.logouts != 0 {
		t.Fatalf("rejected login not paused: snapshot=%+v runtime=%+v", snap, runtime)
	}
	status := a.Status(time.Now(), robotconfig.RuntimeConfig{SchedulerBadFailures: 1})
	if status.Health != HealthUnhealthy || status.HealthReason != "login_rejected" || status.RecycleUID {
		t.Fatalf("rejected character may be recycled: %+v", status)
	}
	result, ok := a.ensureOnlineForCommand()
	if ok || result.OK || !strings.Contains(result.Message, "36") || runtime.logins != 0 {
		t.Fatalf("implicit command retried rejected character: %+v", result)
	}
	if result := a.handleCommand(CommandOnline); result.OK || runtime.logins != 0 {
		t.Fatalf("retry skipped failed logout confirmation: %+v", result)
	}
	runtime.logoutOK = true
	if result := a.handleCommand(CommandOnline); !result.OK {
		t.Fatalf("explicit retry failed: %+v", result)
	}
	a.tick(time.Now())
	if runtime.logins != 1 || !a.snapshot().OnlineDesired {
		t.Fatalf("explicit retry did not log in: %+v logins=%d", a.snapshot(), runtime.logins)
	}
}

type manualAttachRuntime struct {
	partyWaitRuntime
	logins           int
	loginBeforeStore bool
}

func (r *manualAttachRuntime) OnlineNoConfirm(uid int) robotcap.ActionResult {
	r.logins++
	return r.partyWaitRuntime.OnlineNoConfirm(uid)
}

func (r *manualAttachRuntime) Store(uid int) robotcap.ActionResult {
	r.loginBeforeStore = r.logins != 0
	return r.partyWaitRuntime.Store(uid)
}

func TestManualAttachmentWaitsForExplicitLogin(t *testing.T) {
	runtime := &manualAttachRuntime{}
	a := NewActor(1, ModeManual, runtime)
	a.resetForUID(101)
	for i := 0; i < 3; i++ {
		a.tick(time.Now().Add(time.Duration(i) * time.Second))
	}
	if snap := a.snapshot(); snap.OnlineDesired || snap.State != StateOffline || runtime.logins != 0 {
		t.Fatalf("attachment logged in before a command: %+v logins=%d", snap, runtime.logins)
	}
	if result := a.handleCommand(CommandOnline); !result.OK {
		t.Fatalf("online command failed: %+v", result)
	}
	a.tick(time.Now())
	if runtime.logins != 1 || !a.snapshot().OnlineDesired {
		t.Fatalf("explicit online did not log in: snapshot=%+v logins=%d", a.snapshot(), runtime.logins)
	}
}

func TestManualStoreDoesNotPreloginButRetainsOnlineIntent(t *testing.T) {
	runtime := &manualAttachRuntime{}
	a := NewActor(1, ModeManual, runtime)
	a.resetForUID(101)
	a.tick(time.Now())
	result := a.handleCommand(CommandStore)
	if !result.OK || runtime.stores != 1 || runtime.loginBeforeStore || !a.snapshot().OnlineDesired {
		t.Fatalf("store result=%+v stores=%d preliminary_login=%t snapshot=%+v", result, runtime.stores, runtime.loginBeforeStore, a.snapshot())
	}
}

func TestAutomaticAdoptionLogsInAttachedOfflineManualActor(t *testing.T) {
	runtime := &manualAttachRuntime{}
	a := NewActor(1, ModeManual, runtime)
	a.resetForUID(101)
	a.SetMode(ModeAuto)
	a.tick(time.Now())
	if !a.snapshot().OnlineDesired || runtime.logins != 1 {
		t.Fatalf("automatic adoption did not start login: %+v logins=%d", a.snapshot(), runtime.logins)
	}
}

func TestOfflineActorKeepsUIDAttached(t *testing.T) {
	a := NewActor(1, ModeAuto, &partyWaitRuntime{})
	a.resetForUID(101)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired || snap.State != StateAssigned {
		t.Fatalf("assigned snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}

	a.setOnlineDesired(false)
	a.tick(time.Now())
	if snap := a.snapshot(); snap.UID != 101 || snap.OnlineDesired || snap.State != StateOffline {
		t.Fatalf("offline snapshot uid=%d desired=%v state=%s", snap.UID, snap.OnlineDesired, snap.State)
	}

	a.setOnlineDesired(true)
	if snap := a.snapshot(); snap.UID != 101 || !snap.OnlineDesired {
		t.Fatalf("online desired should re-open without detaching uid, uid=%d desired=%v", snap.UID, snap.OnlineDesired)
	}
}

func TestMarkOnlineHealthyClearsHealthWithoutChangingSchedule(t *testing.T) {
	now := time.Now()
	a := NewActor(1, ModeAuto, nil)
	a.resetForUID(101)
	a.failures = 1
	a.firstFailureAt = now.Add(-2 * time.Minute)
	a.lastOnlineTry = now.Add(-2 * time.Minute)
	a.nextStore = now.Add(time.Minute)

	a.markOnlineHealthy()
	snap := a.snapshot()
	if !snap.LastOnlineTry.IsZero() || !snap.FirstFailureAt.IsZero() || snap.Failures != 0 {
		t.Fatalf("healthy actor should clear health state, got last=%s first=%s failures=%d", snap.LastOnlineTry, snap.FirstFailureAt, snap.Failures)
	}
	if !a.nextStore.Equal(now.Add(time.Minute)) {
		t.Fatalf("health reset changed store cooldown: got %s", a.nextStore)
	}
}

func TestActorPollIntervalUsesCurrentNormalizedValue(t *testing.T) {
	tests := []struct {
		milliseconds int
		want         time.Duration
	}{
		{milliseconds: 0, want: time.Second},
		{milliseconds: 10, want: 100 * time.Millisecond},
		{milliseconds: 250, want: 250 * time.Millisecond},
	}
	for _, test := range tests {
		got := actorPollInterval(robotconfig.RuntimeConfig{SystemActorPollMS: test.milliseconds})
		if got != test.want {
			t.Fatalf("actorPollInterval(%d) = %s, want %s", test.milliseconds, got, test.want)
		}
	}
}
