package robotaction

import (
	"testing"

	robotcap "robot/internal/capability/robot"
	"robot/internal/shared"
)

type directLogoutEnv struct {
	selectCalls int
	statusCalls int
	freshCalls  int
	logoutUIDs  []int
}

func (*directLogoutEnv) CountRuntimeRunning() int                    { return 0 }
func (*directLogoutEnv) EnsureWorldHornByCID(int) error              { return nil }
func (*directLogoutEnv) RobotConnectIP() string                      { return "127.0.0.1" }
func (*directLogoutEnv) SendOnline([]shared.RuntimeOnlineUser) error { return nil }

func (e *directLogoutEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	e.statusCalls++
	return map[int]robotcap.RuntimeStatus{17000001: {UID: 17000001}}
}

func (e *directLogoutEnv) RuntimeStatusMapFresh() map[int]robotcap.RuntimeStatus {
	e.freshCalls++
	return nil
}

func (e *directLogoutEnv) SelectRobots(robotcap.CommandRequest) ([]robotcap.Info, error) {
	e.selectCalls++
	return nil, nil
}

func (e *directLogoutEnv) SendLogout(uid int) error {
	e.logoutUIDs = append(e.logoutUIDs, uid)
	return nil
}

func TestLogoutUIDBypassesRepositoryAndCachedStatus(t *testing.T) {
	env := &directLogoutEnv{}
	result, err := (SessionService{Env: env}).LogoutUID(17000001)
	if err != nil {
		t.Fatal(err)
	}
	if env.selectCalls != 0 {
		t.Fatalf("direct logout selected robots %d times", env.selectCalls)
	}
	if env.statusCalls != 0 || env.freshCalls != 1 {
		t.Fatalf("status calls cached=%d fresh=%d", env.statusCalls, env.freshCalls)
	}
	if len(env.logoutUIDs) != 1 || env.logoutUIDs[0] != 17000001 {
		t.Fatalf("logout uids = %v", env.logoutUIDs)
	}
	if result.Confirmed != 1 || len(result.Robots) != 1 || !result.Robots[0].OK || result.Robots[0].State != robotcap.ActionStateClosed {
		t.Fatalf("logout result = %+v", result)
	}
}
