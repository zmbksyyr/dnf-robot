package repository

import (
	"database/sql"
	"errors"
	robotcap "robot/internal/capability/robot"
	"testing"
)

func TestCleanupTableReadySkipsMissingTableWithoutReadingColumns(t *testing.T) {
	columnsCalled := false
	ready, err := cleanupTableReady("db.optional", "uid",
		func(string) (bool, error) { return false, nil },
		func(string) (map[string]bool, error) {
			columnsCalled = true
			return nil, errors.New("columns should not be read")
		},
	)
	if err != nil || ready || columnsCalled {
		t.Fatalf("ready=%t err=%v columns_called=%t", ready, err, columnsCalled)
	}
}

func TestCleanupTableReadyReturnsExistingTableErrors(t *testing.T) {
	want := errors.New("show columns failed")
	ready, err := cleanupTableReady("db.present", "uid",
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]bool, error) { return nil, want },
	)
	if ready || !errors.Is(err, want) {
		t.Fatalf("ready=%t err=%v want=%v", ready, err, want)
	}
}

func TestClassifyRegisteredCleanupCandidateUsesMetadataOnlyForMissingCoreIdentity(t *testing.T) {
	candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
	classifyRegisteredCleanupCandidate(&candidate, false, sql.NullString{}, true, false, 0, 0, false)
	if !candidate.MetadataOnly || candidate.Protected {
		t.Fatalf("candidate = %+v, want metadata-only cleanup", candidate)
	}
}

func TestClassifyRegisteredCleanupCandidateProtectsIdentityConflicts(t *testing.T) {
	tests := []struct {
		name        string
		candidate   robotcap.CleanupCandidate
		account     bool
		accountName sql.NullString
		dummy       bool
		registered  bool
		owner       int
		ownerCount  int
	}{
		{
			name:        "restored real account",
			candidate:   robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"},
			account:     true,
			accountName: sql.NullString{String: "real-player", Valid: true},
			dummy:       true,
		},
		{
			name:       "character remains without account",
			candidate:  robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"},
			dummy:      true,
			ownerCount: 1,
		},
		{
			name:      "registry account mismatch",
			candidate: robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "other"},
			dummy:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifyRegisteredCleanupCandidate(
				&tt.candidate, tt.account, tt.accountName, tt.dummy,
				tt.registered, tt.owner, tt.ownerCount, false,
			)
			if !tt.candidate.Protected || tt.candidate.MetadataOnly {
				t.Fatalf("candidate = %+v, want protected conflict", tt.candidate)
			}
		})
	}
}

func TestClassifyRegisteredCleanupCandidateRequiresExactCharacterOwnership(t *testing.T) {
	tests := []struct {
		name       string
		registered bool
		owner      int
		count      int
		reason     string
	}{
		{name: "registry cid belongs to another uid", registered: true, owner: 42, reason: "registry cid belongs to another uid"},
		{name: "uid owns another cid", count: 1, reason: "uid character does not match registry cid"},
		{name: "uid owns extra characters", registered: true, owner: 17000001, count: 2, reason: "uid has additional characters outside registry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
			classifyRegisteredCleanupCandidate(
				&candidate, true, sql.NullString{String: "17000001", Valid: true}, true,
				tt.registered, tt.owner, tt.count, false,
			)
			if !candidate.Protected || candidate.Reason != tt.reason {
				t.Fatalf("candidate=%+v want protected reason %q", candidate, tt.reason)
			}
		})
	}
}

func TestClassifyRegisteredCleanupCandidateAllowsExactCharacterOwnership(t *testing.T) {
	candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
	classifyRegisteredCleanupCandidate(
		&candidate, true, sql.NullString{String: "17000001", Valid: true}, true,
		true, 17000001, 1, false,
	)
	if candidate.Protected || candidate.MetadataOnly {
		t.Fatalf("candidate=%+v want deletable exact ownership", candidate)
	}
}

func TestClassifyUnregisteredCleanupCandidateProtectsCharacterData(t *testing.T) {
	candidate := robotcap.CleanupCandidate{UID: 17000001, Account: "17000001"}
	classifyUnregisteredCleanupCandidate(&candidate, sql.NullString{String: "17000001", Valid: true}, 1)
	if !candidate.Protected || candidate.Reason != "registry missing but character data exists" {
		t.Fatalf("candidate=%+v", candidate)
	}
}

func TestClassifyLegacyDummyCleanupCandidateAllowsMetadataOnly(t *testing.T) {
	candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
	classifyLegacyDummyCleanupCandidate(&candidate, sql.NullString{}, false, 0, 0)
	if !candidate.MetadataOnly || candidate.Protected || candidate.Reason != "legacy Dummylist metadata only" {
		t.Fatalf("candidate=%+v", candidate)
	}
}

func TestClassifyLegacyDummyCleanupCandidateAllowsExactIdentity(t *testing.T) {
	candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
	classifyLegacyDummyCleanupCandidate(
		&candidate,
		sql.NullString{String: "17000001", Valid: true},
		true,
		17000001,
		1,
	)
	if candidate.MetadataOnly || candidate.Protected {
		t.Fatalf("candidate=%+v", candidate)
	}
}

func TestClassifyLegacyDummyCleanupCandidateProtectsIdentityConflicts(t *testing.T) {
	tests := []struct {
		name           string
		account        sql.NullString
		character      bool
		owner          int
		ownerCount     int
		expectedReason string
	}{
		{name: "account mismatch", account: sql.NullString{String: "real-player", Valid: true}, expectedReason: "accountname does not equal uid"},
		{name: "missing account with character", character: true, owner: 17000001, ownerCount: 1, expectedReason: "account missing but character data exists"},
		{name: "cid belongs to another uid", account: sql.NullString{String: "17000001", Valid: true}, character: true, owner: 42, ownerCount: 1, expectedReason: "Dummylist cid belongs to another uid"},
		{name: "different owned cid", account: sql.NullString{String: "17000001", Valid: true}, ownerCount: 1, expectedReason: "uid character does not match Dummylist cid"},
		{name: "additional character", account: sql.NullString{String: "17000001", Valid: true}, character: true, owner: 17000001, ownerCount: 2, expectedReason: "uid has additional characters outside Dummylist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
			classifyLegacyDummyCleanupCandidate(&candidate, tt.account, tt.character, tt.owner, tt.ownerCount)
			if !candidate.Protected || candidate.MetadataOnly || candidate.Reason != tt.expectedReason {
				t.Fatalf("candidate=%+v", candidate)
			}
		})
	}
}
