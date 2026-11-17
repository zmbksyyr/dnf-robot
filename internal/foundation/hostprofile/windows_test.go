//go:build windows

package hostprofile

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestWindowsProcessIdentityAndStartTime(t *testing.T) {
	previous := Current()
	t.Cleanup(func() { Configure(previous) })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "auction.pid")
	if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	profile := Default(nil)
	profile.Services["auction"] = Service{Executable: exe, PIDFile: marker}
	Configure(profile)
	pid, err := ProcessID("auction")
	if err != nil || pid != os.Getpid() {
		t.Fatalf("pid=%d error=%v", pid, err)
	}
	started, err := ProcessStartTime(pid)
	if err != nil || started.After(time.Now()) || time.Since(started) > 5*time.Minute {
		t.Fatalf("invalid process creation time: %v %v", started, err)
	}
	profile.Services["auction"] = Service{Executable: exe + ".different", PIDFile: marker}
	Configure(profile)
	if _, err := ProcessID("auction"); err == nil {
		t.Fatal("accepted PID belonging to another executable")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if pid, err := ProcessID("auction"); pid != 0 || err != nil {
		t.Fatalf("missing marker: %d %v", pid, err)
	}
}
