package dnf

import (
	"testing"

	"robot/internal/shared"
)

func TestDispatchMoveReportsFullInnerQueue(t *testing.T) {
	drive, task := fullRuntimeCommandDrive(17000001)
	result := drive.DispatchMove(shared.RuntimeMoveCommand{
		UID: 17000001, Village: 3, Area: 1, X: 300, Y: 200, MoveType: 5, Speed: 220,
	})
	if result.Code == 200 || result.Msg != "move queue full" {
		t.Fatalf("move result = %+v", result)
	}
	assertRuntimeCommandQueueUnchanged(t, task, "move")
}

func TestDispatchShoutReportsFullInnerQueue(t *testing.T) {
	drive, task := fullRuntimeCommandDrive(17000002)
	result := drive.DispatchShout(shared.RuntimeShoutCommand{UID: 17000002, Message: "test", Type: 3})
	if result.Code == 200 || result.Msg != "shout queue full" {
		t.Fatalf("shout result = %+v", result)
	}
	assertRuntimeCommandQueueUnchanged(t, task, "shout")
}

func fullRuntimeCommandDrive(uid int) (*DnfTableDrive, *RobotDnfTask) {
	task := newQueueTestTask()
	task.robotVoMap = map[int]*RobotVo{uid: {UID: uint32(uid), State: StateRun}}
	shard := task.messageShards[messageShardIndex("MsgMove", &moveInternalData{ID: uid})]
	for i := 0; i < messageShardQueueSize; i++ {
		shard.queue = append(shard.queue, MsgQueueData{Type: "MsgOnLine", Data: &RobotVo{UID: uint32(uid + i + 1)}})
	}
	return &DnfTableDrive{task: task}, task
}

func assertRuntimeCommandQueueUnchanged(t *testing.T, task *RobotDnfTask, command string) {
	t.Helper()
	queued := 0
	for _, shard := range task.messageShards {
		queued += len(shard.queue)
	}
	if queued != messageShardQueueSize {
		t.Fatalf("%s queue length = %d, want %d", command, queued, messageShardQueueSize)
	}
}
