package process

import (
	"errors"
	"strings"
	"testing"
)

func TestRequiredOpenFiles(t *testing.T) {
	got, err := RequiredOpenFiles(550, 64)
	if err != nil {
		t.Fatalf("RequiredOpenFiles() error = %v", err)
	}
	if want := uint64(1420); got != want {
		t.Fatalf("RequiredOpenFiles() = %d, want %d", got, want)
	}
}

func TestRequiredOpenFilesRejectsInvalidCapacity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		robots   int
		database int
	}{
		{name: "robots", robots: 0, database: 64},
		{name: "database", robots: 550, database: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RequiredOpenFiles(tc.robots, tc.database); err == nil {
				t.Fatal("RequiredOpenFiles() error = nil")
			}
		})
	}
}

func TestEnsureOpenFileLimitLeavesSufficientLimitAlone(t *testing.T) {
	api := &fakeFileLimitAPI{limit: fileLimit{soft: 2048, hard: 4096}}
	if err := ensureOpenFileLimit(api, 550, 64); err != nil {
		t.Fatalf("ensureOpenFileLimit() error = %v", err)
	}
	if len(api.sets) != 0 {
		t.Fatalf("set calls = %v, want none", api.sets)
	}
}

func TestEnsureOpenFileLimitRaisesSoftLimit(t *testing.T) {
	api := &fakeFileLimitAPI{limit: fileLimit{soft: 1024, hard: 4096}, applySet: true}
	if err := ensureOpenFileLimit(api, 550, 64); err != nil {
		t.Fatalf("ensureOpenFileLimit() error = %v", err)
	}
	want := fileLimit{soft: 1420, hard: 4096}
	if len(api.sets) != 1 || api.sets[0] != want {
		t.Fatalf("set calls = %v, want [%v]", api.sets, want)
	}
}

func TestEnsureOpenFileLimitAttemptsToRaiseHardLimit(t *testing.T) {
	api := &fakeFileLimitAPI{limit: fileLimit{soft: 1024, hard: 1200}, applySet: true}
	if err := ensureOpenFileLimit(api, 550, 64); err != nil {
		t.Fatalf("ensureOpenFileLimit() error = %v", err)
	}
	want := fileLimit{soft: 1420, hard: 1420}
	if len(api.sets) != 1 || api.sets[0] != want {
		t.Fatalf("set calls = %v, want [%v]", api.sets, want)
	}
}

func TestEnsureOpenFileLimitReportsUnsatisfiedCapacity(t *testing.T) {
	api := &fakeFileLimitAPI{
		limit:  fileLimit{soft: 1024, hard: 1200},
		setErr: errors.New("operation not permitted"),
	}
	err := ensureOpenFileLimit(api, 550, 64)
	if err == nil {
		t.Fatal("ensureOpenFileLimit() error = nil")
	}
	for _, detail := range []string{"required=1420", "soft=1024", "hard=1200", "operation not permitted"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("ensureOpenFileLimit() error = %q, want detail %q", err, detail)
		}
	}
}

type fakeFileLimitAPI struct {
	limit    fileLimit
	getErr   error
	setErr   error
	applySet bool
	sets     []fileLimit
}

func (f *fakeFileLimitAPI) get() (fileLimit, error) {
	if f.getErr != nil {
		return fileLimit{}, f.getErr
	}
	return f.limit, nil
}

func (f *fakeFileLimitAPI) set(limit fileLimit) error {
	f.sets = append(f.sets, limit)
	if f.setErr == nil && f.applySet {
		f.limit = limit
	}
	return f.setErr
}
