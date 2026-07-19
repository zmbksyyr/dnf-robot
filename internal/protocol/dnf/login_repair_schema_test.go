package dnf

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

func TestLoginRepairCapabilityCacheLoadsOncePerDatabase(t *testing.T) {
	var cache loginRepairCapabilityCache
	db1 := new(sql.DB)
	db2 := new(sql.DB)
	calls := 0
	load := func(*sql.DB) (loginRepairCapabilities, error) {
		calls++
		return loginRepairCapabilities{robotRegistry: true}, nil
	}

	for i := 0; i < 2; i++ {
		capabilities, err := cache.get(db1, load)
		if err != nil || !capabilities.robotRegistry {
			t.Fatalf("db1 load %d: capabilities=%+v err=%v", i, capabilities, err)
		}
	}
	if _, err := cache.get(db2, load); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("loader calls=%d want 2", calls)
	}
}

func TestLoginRepairCapabilityCacheRetriesFailure(t *testing.T) {
	var cache loginRepairCapabilityCache
	db := new(sql.DB)
	calls := 0
	load := func(*sql.DB) (loginRepairCapabilities, error) {
		calls++
		if calls == 1 {
			return loginRepairCapabilities{}, errors.New("temporary failure")
		}
		return loginRepairCapabilities{memberPunishInfo: true}, nil
	}

	if _, err := cache.get(db, load); err == nil {
		t.Fatal("initial load unexpectedly succeeded")
	}
	capabilities, err := cache.get(db, load)
	if err != nil || !capabilities.memberPunishInfo || calls != 2 {
		t.Fatalf("retry capabilities=%+v calls=%d err=%v", capabilities, calls, err)
	}
}

func TestLoginRepairCapabilityTableOrder(t *testing.T) {
	capabilities := loginRepairCapabilities{
		dTaiwanMemberJoinInfo:          true,
		taiwanLoginMemberJoinInfo:      true,
		dTaiwanMemberSecurityGrade:     true,
		dTaiwanSecuMemberSecurityGrade: true,
	}
	if got, want := capabilities.memberJoinInfoTables(), []string{"d_taiwan.member_join_info", "taiwan_login.member_join_info"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("member join tables=%v want %v", got, want)
	}
	if got, want := capabilities.memberSecurityGradeTables(), []string{"d_taiwan.member_security_grade", "d_taiwan_secu.member_security_grade"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("security grade tables=%v want %v", got, want)
	}
}
