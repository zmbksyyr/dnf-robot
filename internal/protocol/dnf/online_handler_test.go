package dnf

import (
	"math"
	"testing"

	"robot/internal/shared"
)

func TestValidOnlineUserSeparatesDatabaseCIDFromCharacterSlot(t *testing.T) {
	valid := shared.RuntimeOnlineUser{
		UID: 17000001, CID: 900001, CharacterSlot: 7, IP: "127.0.0.1", Port: 10011,
		MaxReconnect: 2, ReconnectDelay: 5000,
		BirthVillage: 1, BirthArea: 2, BirthGateArea: 3, BirthX: 100, BirthY: 200,
	}
	if !validOnlineUser(valid) {
		t.Fatal("valid online user was rejected")
	}

	type testCase struct {
		name   string
		mutate func(*shared.RuntimeOnlineUser)
	}
	tests := []testCase{
		{name: "zero cid", mutate: func(user *shared.RuntimeOnlineUser) { user.CID = 0 }},
		{name: "negative cid", mutate: func(user *shared.RuntimeOnlineUser) { user.CID = -1 }},
		{name: "slot negative", mutate: func(user *shared.RuntimeOnlineUser) { user.CharacterSlot = -1 }},
		{name: "slot overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.CharacterSlot = math.MaxUint8 + 1 }},
		{name: "village negative", mutate: func(user *shared.RuntimeOnlineUser) { user.BirthVillage = -1 }},
		{name: "area overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.BirthArea = math.MaxUint8 + 1 }},
		{name: "gate overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.BirthGateArea = math.MaxUint8 + 1 }},
		{name: "x negative", mutate: func(user *shared.RuntimeOnlineUser) { user.BirthX = -1 }},
		{name: "y overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.BirthY = math.MaxUint16 + 1 }},
	}
	if uint64(^uint(0)) > math.MaxUint32 {
		tests = append(tests,
			testCase{name: "uid overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.UID = int(uint64(math.MaxUint32) + 1) }},
			testCase{name: "cid overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.CID = int(uint64(math.MaxUint32) + 1) }},
			testCase{name: "reconnect delay overflow", mutate: func(user *shared.RuntimeOnlineUser) { user.ReconnectDelay = int(uint64(math.MaxUint32) + 1) }},
		)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := valid
			test.mutate(&user)
			if validOnlineUser(user) {
				t.Fatalf("invalid online user was accepted: %+v", user)
			}
		})
	}
}
