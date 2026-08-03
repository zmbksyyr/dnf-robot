package scheduler

import (
	"net"
	"testing"
	"time"

	robotconfig "robot/internal/capability/robotconfig"
	"robot/internal/foundation/config"
)

func TestAutoGamePortProbeUsesBoundedCache(t *testing.T) {
	m := NewRobotManager(nil, &config.SysConfig{RobotConnectIP: "127.0.0.1", RobotGamePort: 10011}, nil)
	calls := 0
	m.autoPortDial = func(_, _ string, _ time.Duration) (net.Conn, error) {
		calls++
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
	rc := robotconfig.RuntimeConfig{AutoGamePortCheckTimeoutMS: 10, AutoGamePortStableSec: 1}
	now := time.Unix(100, 0)
	_ = m.autoGamePortStable(now, rc)
	_ = m.autoGamePortStable(now.Add(500*time.Millisecond), rc)
	if calls != 1 {
		t.Fatalf("dial calls within cache TTL = %d, want 1", calls)
	}
	_ = m.autoGamePortStable(now.Add(1100*time.Millisecond), rc)
	if calls != 2 {
		t.Fatalf("dial calls after cache TTL = %d, want 2", calls)
	}
}
