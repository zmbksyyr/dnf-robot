package scheduler

import (
	"strings"
	"testing"

	robotcap "robot/internal/capability/robot"
)

type savingLogoutRuntime struct {
	noopRuntime
	state    string
	rejected bool
	events   []string
}

func (r *savingLogoutRuntime) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return map[int]robotcap.RuntimeStatus{17000001: {UID: 17000001, StateName: r.state}}
}

func (r *savingLogoutRuntime) ReturnToCharacterSelect(int) bool {
	r.events = append(r.events, "save")
	if !r.rejected {
		r.state = robotcap.RuntimeStateSelect
	}
	return true
}

func (r *savingLogoutRuntime) AtCharacterSelect(int) bool {
	r.events = append(r.events, "ack")
	return r.state == robotcap.RuntimeStateSelect
}

func (r *savingLogoutRuntime) CharacterSelectRejected(int) bool { return r.rejected }
func (*savingLogoutRuntime) ReselectCharacter(int) bool         { return false }

func (r *savingLogoutRuntime) Logout(int) error {
	r.events = append(r.events, "close")
	r.state = robotcap.RuntimeStateStop
	return nil
}

func TestNormalLogoutSavesBeforeClosingDuringShutdown(t *testing.T) {
	runtime := &savingLogoutRuntime{state: robotcap.RuntimeStateRunning}
	m := NewRobotManager(nil, nil, runtime)
	m.RequestShutdown()
	if err := (sessionActionEnv{manager: m}).SendLogout(17000001); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runtime.events, ","); got != "save,ack,close" {
		t.Fatalf("logout order = %q", got)
	}
	// Finishing the logout save must not grant permission for database writes.
	if _, err := m.refreshCharacterForWrite(17000001, nil); err == nil {
		t.Fatal("shutdown granted a character write boundary")
	}
}

func TestNormalLogoutReportsRejectedSaveWithoutClaimingClosure(t *testing.T) {
	runtime := &savingLogoutRuntime{state: robotcap.RuntimeStateRunning, rejected: true}
	m := NewRobotManager(nil, nil, runtime)
	err := (sessionActionEnv{manager: m}).SendLogout(17000001)
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("save error = %v", err)
	}
	if got := strings.Join(runtime.events, ","); got != "save" {
		t.Fatalf("rejected save order = %q", got)
	}
}
