package dnfruntime

import (
	"errors"
	"sync"
	"testing"
	"time"

	"robot/internal/foundation/lockhub"
	"robot/internal/protocol/dnf"
	"robot/internal/shared"
)

type recordedCommand struct {
	typ    robotCommandType
	uid    int
	move   shared.RuntimeMoveCommand
	online []shared.RuntimeOnlineUser
	shout  shared.RuntimeShoutCommand
}

type recordingDriver struct {
	mu       lockhub.Locker
	calls    []recordedCommand
	notify   chan struct{}
	release  <-chan struct{}
	shutdown int
}

func (d *recordingDriver) DispatchLogout(uid int) dnf.DnfTableTaskResult {
	d.record(recordedCommand{typ: robotCommandLogout, uid: uid})
	return dnf.DnfTableTaskResult{Code: 200}
}

func (d *recordingDriver) DispatchMove(command shared.RuntimeMoveCommand) dnf.DnfTableTaskResult {
	d.record(recordedCommand{typ: robotCommandMove, move: command})
	return dnf.DnfTableTaskResult{Code: 200}
}

func (d *recordingDriver) DispatchOnline(users []shared.RuntimeOnlineUser) dnf.DnfTableTaskResult {
	if d.release != nil {
		<-d.release
	}
	d.record(recordedCommand{typ: robotCommandOnline, online: append([]shared.RuntimeOnlineUser(nil), users...)})
	return dnf.DnfTableTaskResult{Code: 200}
}

func (d *recordingDriver) DispatchShout(command shared.RuntimeShoutCommand) dnf.DnfTableTaskResult {
	d.record(recordedCommand{typ: robotCommandShout, shout: command})
	return dnf.DnfTableTaskResult{Code: 200}
}

func (d *recordingDriver) GetTask() *dnf.RobotDnfTask { return nil }

func (d *recordingDriver) Shutdown() {
	d.mu.Lock()
	d.shutdown++
	d.mu.Unlock()
}

func (d *recordingDriver) record(call recordedCommand) {
	d.mu.Lock()
	d.calls = append(d.calls, call)
	d.mu.Unlock()
	if d.notify != nil {
		d.notify <- struct{}{}
	}
}

func (d *recordingDriver) snapshot() ([]recordedCommand, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]recordedCommand(nil), d.calls...), d.shutdown
}

func TestRobotServiceDispatchesTypedCommandsOnceInOrder(t *testing.T) {
	driver := &recordingDriver{notify: make(chan struct{}, 4)}
	service := newRobotService(driver)

	move := shared.RuntimeMoveCommand{UID: 17000001, Village: 1, Area: 2, X: 300, Y: 400, MoveType: 1, Speed: 250}
	users := []shared.RuntimeOnlineUser{{UID: 17000002, IP: "127.0.0.1", Port: 10012}}
	shout := shared.RuntimeShoutCommand{UID: 17000003, Message: "hello", Type: 3}
	if err := service.Logout(17000001); err != nil {
		t.Fatal(err)
	}
	if err := service.Move(move); err != nil {
		t.Fatal(err)
	}
	if err := service.Online(users); err != nil {
		t.Fatal(err)
	}
	if err := service.Shout(shout); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		select {
		case <-driver.notify:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for typed command dispatch")
		}
	}
	service.Shutdown()

	calls, shutdown := driver.snapshot()
	if len(calls) != 4 {
		t.Fatalf("dispatch calls = %d, want 4", len(calls))
	}
	wantTypes := []robotCommandType{robotCommandLogout, robotCommandMove, robotCommandOnline, robotCommandShout}
	for i, want := range wantTypes {
		if calls[i].typ != want {
			t.Fatalf("call %d type = %s, want %s", i, calls[i].typ, want)
		}
	}
	if calls[0].uid != 17000001 || calls[1].move != move || calls[2].online[0] != users[0] || calls[3].shout != shout {
		t.Fatalf("typed command payloads changed: %+v", calls)
	}
	if shutdown != 1 {
		t.Fatalf("driver shutdown calls = %d, want 1", shutdown)
	}
	if err := service.Logout(17000001); !errors.Is(err, ErrRuntimeStopped) {
		t.Fatalf("enqueue after shutdown error = %v, want %v", err, ErrRuntimeStopped)
	}
}

func TestRobotServiceCopiesOnlineCommandBeforeQueueing(t *testing.T) {
	release := make(chan struct{})
	driver := &recordingDriver{notify: make(chan struct{}, 1), release: release}
	service := newRobotService(driver)
	users := []shared.RuntimeOnlineUser{{UID: 17000001, IP: "127.0.0.1", Port: 10011}}
	if err := service.Online(users); err != nil {
		t.Fatal(err)
	}
	users[0].UID = 99
	close(release)
	select {
	case <-driver.notify:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for online dispatch")
	}
	service.Shutdown()

	calls, _ := driver.snapshot()
	if got := calls[0].online[0].UID; got != 17000001 {
		t.Fatalf("queued online uid = %d, want 17000001", got)
	}
}

func TestRobotServiceRejectsFullBoundedQueue(t *testing.T) {
	service := &RobotSvc{running: true, table: &recordingDriver{}}
	service.cond = sync.NewCond(&service.mu)
	service.msgQueue = make([]robotMsgEntry, maxRobotCommandQueue)
	if err := service.Logout(17000001); !errors.Is(err, ErrCommandQueueFull) {
		t.Fatalf("full queue error = %v, want %v", err, ErrCommandQueueFull)
	}
	if got := len(service.msgQueue); got != maxRobotCommandQueue {
		t.Fatalf("queue length = %d, want %d", got, maxRobotCommandQueue)
	}
}
