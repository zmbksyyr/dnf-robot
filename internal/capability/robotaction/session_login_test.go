package robotaction

import (
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

func TestOnlineConfirmationStopsWithoutClaimingSuccessDuringShutdown(t *testing.T) {
	service := SessionService{Env: &directLogoutEnv{}, Cancelled: func() bool { return true }}
	result := robotcap.NewCommandResult(1)
	result.Accepted = 1
	result.Robots = []robotcap.ActionResult{{UID: 17000001, State: robotcap.ActionStateAccepted}}
	started := time.Now()
	service.ConfirmAccepted(&result, time.Minute)
	if time.Since(started) > time.Second || result.Confirmed != 0 || result.Failed != 1 {
		t.Fatalf("cancelled confirmation = %+v", result)
	}
}

func TestOnlinePayloadKeepsDatabaseCIDSeparateFromCharacterSlot(t *testing.T) {
	service := SessionService{Env: &directLogoutEnv{}}
	got := service.onlinePayload(robotcap.Info{
		UID:           17000001,
		CID:           900001,
		CharacterSlot: 7,
	}, 0, robotconfig.RuntimeConfig{})

	if got.CID != 900001 {
		t.Fatalf("online cid = %d, want database charac_no 900001", got.CID)
	}
	if got.CharacterSlot != 7 {
		t.Fatalf("online character slot = %d, want resolved slot 7", got.CharacterSlot)
	}
}

func TestOnlinePayloadCarriesPersistentGuildMembership(t *testing.T) {
	service := SessionService{Env: &directLogoutEnv{}}
	got := service.onlinePayload(robotcap.Info{UID: 17000001, CID: 900001, GuildID: 2}, 0, robotconfig.RuntimeConfig{})
	if got.GuildID != 2 {
		t.Fatalf("online guild id = %d, want 2", got.GuildID)
	}
}

func TestOnlinePayloadUsesConfiguredGamePortInsteadOfStoredRobotPort(t *testing.T) {
	service := SessionService{Env: &directLogoutEnv{}}
	got := service.onlinePayload(robotcap.Info{
		UID:  17000001,
		CID:  900001,
		Port: 10011,
	}, 0, robotconfig.RuntimeConfig{})

	if got.Port != 20011 {
		t.Fatalf("online port = %d, want configured game port 20011", got.Port)
	}
}
