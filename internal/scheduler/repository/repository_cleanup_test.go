package repository

import (
	"database/sql"
	robotcap "robot/internal/capability/robot"
	"testing"
)

func TestClassifyRegisteredCleanupCandidateUsesMetadataOnlyForMissingCoreIdentity(t *testing.T) {
	candidate := robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"}
	classifyRegisteredCleanupCandidate(&candidate, false, sql.NullString{}, true, false, false)
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
		core        bool
	}{
		{
			name:        "restored real account",
			candidate:   robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"},
			account:     true,
			accountName: sql.NullString{String: "real-player", Valid: true},
			dummy:       true,
		},
		{
			name:      "character remains without account",
			candidate: robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "17000001"},
			dummy:     true,
			core:      true,
		},
		{
			name:      "registry account mismatch",
			candidate: robotcap.CleanupCandidate{UID: 17000001, CID: 900001, Account: "other"},
			dummy:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifyRegisteredCleanupCandidate(&tt.candidate, tt.account, tt.accountName, tt.dummy, tt.core, false)
			if !tt.candidate.Protected || tt.candidate.MetadataOnly {
				t.Fatalf("candidate = %+v, want protected conflict", tt.candidate)
			}
		})
	}
}
