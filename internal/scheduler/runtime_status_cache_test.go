package scheduler

import (
	robotcap "robot/internal/capability/robot"
	"sync"
	"sync/atomic"
	"testing"
)

type countingStatusRuntime struct {
	noopRuntime
	statuses []robotcap.RuntimeStatus
	calls    atomic.Int32
	started  chan struct{}
	release  chan struct{}
}

func (r *countingStatusRuntime) RuntimeStatus() []robotcap.RuntimeStatus {
	r.calls.Add(1)
	if r.started != nil {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}
	if r.release != nil {
		<-r.release
	}
	return append([]robotcap.RuntimeStatus(nil), r.statuses...)
}

func TestRuntimeStatusRefreshIsSingleflight(t *testing.T) {
	runtime := &countingStatusRuntime{
		statuses: []robotcap.RuntimeStatus{{UID: 17000001, StateName: robotcap.RuntimeStateRunning}},
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
	manager := NewRobotManager(nil, nil, runtime)

	const readers = 64
	start := make(chan struct{})
	errs := make(chan string, readers)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			status, ok := manager.runtimeStatus(17000001)
			if !ok || status.UID != 17000001 {
				errs <- "runtime status missing"
			}
		}()
	}
	close(start)
	<-runtime.started
	close(runtime.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := runtime.calls.Load(); got != 1 {
		t.Fatalf("RuntimeStatus calls got %d want 1", got)
	}
}

func TestRuntimeStatusMapCopyDoesNotMutateSnapshot(t *testing.T) {
	runtime := &countingStatusRuntime{statuses: []robotcap.RuntimeStatus{
		{UID: 17000001, StateName: robotcap.RuntimeStateRunning},
		{UID: 17000002, StateName: robotcap.RuntimeStateLogin},
	}}
	manager := NewRobotManager(nil, nil, runtime)

	mutable := manager.runtimeStatusMapCopy()
	delete(mutable, 17000001)
	mutable[17000003] = robotcap.RuntimeStatus{UID: 17000003}

	snapshot := manager.runtimeStatusMap()
	if _, ok := snapshot[17000001]; !ok {
		t.Fatal("deleting from mutable copy changed cached snapshot")
	}
	if _, ok := snapshot[17000003]; ok {
		t.Fatal("adding to mutable copy changed cached snapshot")
	}
	if got := runtime.calls.Load(); got != 1 {
		t.Fatalf("RuntimeStatus calls got %d want 1", got)
	}
}

func TestRuntimeStatusRejectsInvalidUIDWithoutRefresh(t *testing.T) {
	runtime := &countingStatusRuntime{}
	manager := NewRobotManager(nil, nil, runtime)
	if _, ok := manager.runtimeStatus(0); ok {
		t.Fatal("zero UID unexpectedly resolved")
	}
	if got := runtime.calls.Load(); got != 0 {
		t.Fatalf("RuntimeStatus calls got %d want 0", got)
	}
}

func TestCountRuntimeRunningUsesCachedSnapshot(t *testing.T) {
	runtime := &countingStatusRuntime{statuses: []robotcap.RuntimeStatus{
		{UID: 17000001, StateName: robotcap.RuntimeStateRunning},
		{UID: 17000002, StateName: robotcap.RuntimeStateLogin},
	}}
	manager := NewRobotManager(nil, nil, runtime)
	if got := manager.countRuntimeRunning(); got != 1 {
		t.Fatalf("running count = %d", got)
	}
	if got := manager.countRuntimeRunning(); got != 1 {
		t.Fatalf("cached running count = %d", got)
	}
	if got := runtime.calls.Load(); got != 1 {
		t.Fatalf("RuntimeStatus calls got %d want 1", got)
	}
}

func BenchmarkRuntimeStatusLookup550(b *testing.B) {
	manager := benchmarkRuntimeStatusManager(b)
	if _, ok := manager.runtimeStatus(17000275); !ok {
		b.Fatal("benchmark status missing")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.runtimeStatus(17000275)
	}
}

func BenchmarkRuntimeStatusMutableCopy550(b *testing.B) {
	manager := benchmarkRuntimeStatusManager(b)
	_ = manager.runtimeStatusMap()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.runtimeStatusMapCopy()
	}
}

func benchmarkRuntimeStatusManager(b *testing.B) *RobotManager {
	b.Helper()
	statuses := make([]robotcap.RuntimeStatus, 550)
	for i := range statuses {
		statuses[i] = robotcap.RuntimeStatus{UID: 17000000 + i, StateName: robotcap.RuntimeStateRunning}
	}
	return NewRobotManager(nil, nil, &countingStatusRuntime{statuses: statuses})
}
