package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	robotcap "robot/internal/capability/robot"
)

type positionExecCall struct {
	query string
	args  []interface{}
}

func makePositionUpdates(count int) []robotcap.PositionUpdate {
	updates := make([]robotcap.PositionUpdate, count)
	for index := range updates {
		uid := 1000 + index
		updates[index] = robotcap.PositionUpdate{
			UID:         uid,
			CID:         2000 + index,
			FromVillage: 1,
			FromArea:    2,
			FromX:       index,
			FromY:       index + 1,
			Village:     3,
			Area:        4,
			X:           index + 10,
			Y:           index + 11,
		}
	}
	return updates
}

func TestExecuteRobotPositionUpdatesSplitsAtBatchBoundary(t *testing.T) {
	updates := makePositionUpdates(robotPositionBatchSize + 1)
	var calls []positionExecCall
	err := executeRobotPositionUpdates(context.Background(), updates, func(_ context.Context, query string, args ...interface{}) (sql.Result, error) {
		calls = append(calls, positionExecCall{query: query, args: append([]interface{}(nil), args...)})
		return nil, nil
	})
	if err != nil {
		t.Fatalf("execute position updates: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("exec calls got %d want 2", len(calls))
	}
	if got := len(calls[0].args); got != robotPositionBatchSize*10 {
		t.Fatalf("first args got %d want %d", got, robotPositionBatchSize*10)
	}
	if got := len(calls[1].args); got != 10 {
		t.Fatalf("second args got %d want 10", got)
	}
	if got, ok := calls[0].args[0].(string); !ok || got != "1000" {
		t.Fatalf("first UID arg got %#v", calls[0].args[0])
	}
	if got, ok := calls[0].args[1].(string); !ok || got != "2000" {
		t.Fatalf("first CID arg got %#v", calls[0].args[1])
	}
	if got, ok := calls[1].args[0].(string); !ok || got != "1128" {
		t.Fatalf("second batch UID arg got %#v", calls[1].args[0])
	}
	if unions := strings.Count(calls[0].query, "UNION ALL SELECT"); unions != robotPositionBatchSize-1 {
		t.Fatalf("first query unions got %d want %d", unions, robotPositionBatchSize-1)
	}
}

func TestBuildRobotPositionUpdateHasOptimisticGuards(t *testing.T) {
	query, args := buildRobotPositionUpdate(makePositionUpdates(1))
	guards := []string{
		"ON d.UID=p.uid AND d.CID=p.cid",
		"d.function_type='0'",
		"d.curvill=p.fromvill",
		"d.curarea=p.fromarea",
		"d.curx=p.fromx",
		"d.cury=p.fromy",
	}
	for _, guard := range guards {
		if !strings.Contains(query, guard) {
			t.Fatalf("query missing guard %q: %s", guard, query)
		}
	}
	if len(args) != 10 {
		t.Fatalf("args got %d want 10", len(args))
	}
}

func TestExecuteRobotPositionUpdatesStopsAtFirstError(t *testing.T) {
	wantErr := errors.New("write failed")
	calls := 0
	err := executeRobotPositionUpdates(context.Background(), makePositionUpdates(robotPositionBatchSize*3), func(context.Context, string, ...interface{}) (sql.Result, error) {
		calls++
		if calls == 2 {
			return nil, wantErr
		}
		return nil, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("execute error got %v want %v", err, wantErr)
	}
	if calls != 2 {
		t.Fatalf("exec calls got %d want 2", calls)
	}
}

func TestExecuteRobotPositionUpdatesEmptyBatchDoesNotExec(t *testing.T) {
	calls := 0
	err := executeRobotPositionUpdates(context.Background(), nil, func(context.Context, string, ...interface{}) (sql.Result, error) {
		calls++
		return nil, nil
	})
	if err != nil {
		t.Fatalf("execute empty updates: %v", err)
	}
	if calls != 0 {
		t.Fatalf("exec calls got %d want 0", calls)
	}
}

func TestExecuteRobotPositionUpdatesHonorsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := executeRobotPositionUpdates(ctx, makePositionUpdates(1), func(ctx context.Context, _ string, _ ...interface{}) (sql.Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("execute error got %v want deadline exceeded", err)
	}
}
