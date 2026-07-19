package scheduler

import (
	"context"
	"database/sql"
	"testing"

	"robot/internal/foundation/config"
)

type loginRepairInvalidationRuntime struct {
	noopRuntime
	calls [][]int
}

func (r *loginRepairInvalidationRuntime) InvalidateLoginRepairs(uids []int) {
	r.calls = append(r.calls, append([]int(nil), uids...))
}

type loginRepairInvalidationRepository struct {
	missingSchemaRepository
}

func (*loginRepairInvalidationRepository) Stats() sql.DBStats                { return sql.DBStats{} }
func (*loginRepairInvalidationRepository) PingContext(context.Context) error { return nil }
func (*loginRepairInvalidationRepository) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}
func (*loginRepairInvalidationRepository) EnsureAccount(int, string) error { return nil }
func (*loginRepairInvalidationRepository) BatchDeleteRobotData([]int, []int) error {
	return nil
}

func TestLifecycleInvalidatesLoginRepairCacheForCreateAndCleanup(t *testing.T) {
	runtime := &loginRepairInvalidationRuntime{}
	repo := &loginRepairInvalidationRepository{}
	manager := NewRobotManager(repo, &config.SysConfig{ConfigDir: t.TempDir()}, runtime)
	t.Cleanup(func() { _ = manager.Shutdown() })

	if err := (lifecycleCreateEnv{manager: manager}).EnsureAccount(17000001, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := (lifecycleCleanupEnv{manager: manager}).BatchDeleteRobotData([]int{17000001, 17000002}, []int{900001, 900002}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.calls) != 2 || len(runtime.calls[0]) != 1 || runtime.calls[0][0] != 17000001 || len(runtime.calls[1]) != 2 {
		t.Fatalf("login repair invalidations = %v", runtime.calls)
	}
}
