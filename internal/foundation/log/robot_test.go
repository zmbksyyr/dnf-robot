package log

import (
	"io"
	"os"
	"testing"
)

func TestRobotfUsesSinkWithoutDuplicatingStdout(t *testing.T) {
	oldStdout := os.Stdout
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writeEnd
	defer func() {
		os.Stdout = oldStdout
		SetRobotSink(nil)
		_ = readEnd.Close()
		_ = writeEnd.Close()
	}()

	var sinkMessage string
	SetRobotSink(func(msg string) {
		sinkMessage = msg
	})
	Robotf("robot %d\n", 7)
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}
	if sinkMessage != "robot 7\n" {
		t.Fatalf("sink message = %q", sinkMessage)
	}
	if len(stdout) != 0 {
		t.Fatalf("stdout duplicated sink message: %q", stdout)
	}
}
