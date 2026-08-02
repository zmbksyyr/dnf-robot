package webadmin

import (
	"strings"
	"testing"
)

func TestServerScriptDispatchDoesNotStayRunning(t *testing.T) {
	original := launchDetachedServerScript
	t.Cleanup(func() { launchDetachedServerScript = original })

	var launched []string
	canceled := 0
	launchDetachedServerScript = func(script string) (func(), error) {
		launched = append(launched, script)
		return func() { canceled++ }, nil
	}

	s := &Server{}
	runStatus := s.startServerScript("run", "/root/run")
	if !runStatus.OK || runStatus.Running {
		t.Fatalf("run status=%+v", runStatus)
	}
	stopStatus := s.startServerScript("stop", "/root/stop")
	if !stopStatus.OK || stopStatus.Running {
		t.Fatalf("stop status=%+v", stopStatus)
	}
	if canceled != 1 {
		t.Fatalf("startup cancel count=%d, want 1", canceled)
	}
	if len(launched) != 2 || launched[0] != "/root/run" || launched[1] != "/root/stop" {
		t.Fatalf("launched=%v", launched)
	}
}

func TestServerScriptButtonsRemainAvailableAfterDispatch(t *testing.T) {
	if strings.Contains(appJS, "(running?'disabled'") {
		t.Fatal("server script buttons are still tied to a long-running state")
	}
	if !strings.Contains(appJS, "Submitted ") {
		t.Fatal("server script dialog does not report the last submitted command")
	}
}

func TestStopServerScriptCancelsCurrentDispatcherOnce(t *testing.T) {
	canceled := 0
	s := &Server{serverScriptCancel: func() { canceled++ }}
	s.stopServerScript()
	s.stopServerScript()
	if canceled != 1 {
		t.Fatalf("dispatcher canceled %d times, want 1", canceled)
	}
}
