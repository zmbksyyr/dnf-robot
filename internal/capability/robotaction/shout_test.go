package robotaction

import (
	"errors"
	"testing"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
	robottemplate "robot/internal/capability/robottemplate"
)

type failingShoutEnv struct {
	worldErr error
	localErr error
}

func (*failingShoutEnv) AppendShout(int, string, int, string) error { return nil }
func (*failingShoutEnv) AppendShoutDetail(int, string, int, string, string) error {
	return nil
}
func (*failingShoutEnv) Config() robotconfig.RuntimeConfig {
	return robotconfig.RuntimeConfig{ShoutSendEnabled: true}
}
func (*failingShoutEnv) LookupRobotName(int) string { return "robot" }
func (*failingShoutEnv) RandBetween(min, _ int) int { return min }
func (*failingShoutEnv) RandIntn(int) int           { return 0 }
func (*failingShoutEnv) RuntimeStatusMap() map[int]robotcap.RuntimeStatus {
	return nil
}
func (*failingShoutEnv) SelectRobots(robotcap.CommandRequest) ([]robotcap.Info, error) {
	return nil, nil
}
func (e *failingShoutEnv) SendLocalShout(string, int, string, int) error { return e.localErr }
func (e *failingShoutEnv) SendWorldShout(string, string, uint16) error   { return e.worldErr }
func (*failingShoutEnv) Templates() robottemplate.ShoutTemplates {
	return robottemplate.ShoutTemplates{Messages: []string{"hello"}}
}

func TestAutoShoutReturnsTransportErrors(t *testing.T) {
	want := errors.New("monitor unavailable")
	service := ShoutService{Env: &failingShoutEnv{worldErr: want}}
	if err := service.AutoShout(17000001, "hello", true); !errors.Is(err, want) {
		t.Fatalf("AutoShout error = %v, want %v", err, want)
	}
}
