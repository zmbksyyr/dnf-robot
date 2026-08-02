package robotaction

import (
	"testing"

	robotcap "robot/internal/capability/robot"
	robotconfig "robot/internal/capability/robotconfig"
)

func TestOnlinePayloadKeepsDatabaseCIDSeparateFromCharacterSlot(t *testing.T) {
	service := SessionService{Env: &directLogoutEnv{}}
	got := service.onlinePayload(robotcap.Info{
		UID: 17000001,
		CID: 900001,
	}, 0, robotconfig.RuntimeConfig{})

	if got.CID != 900001 {
		t.Fatalf("online cid = %d, want database charac_no 900001", got.CID)
	}
	if got.CharacterSlot != 0 {
		t.Fatalf("online character slot = %d, want first slot 0", got.CharacterSlot)
	}
}
