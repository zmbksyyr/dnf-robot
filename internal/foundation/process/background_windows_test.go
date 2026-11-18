//go:build windows

package process

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestBackgroundCommandHasNoConsoleAndKeepsOutput(t *testing.T) {
	if os.Getenv("ROBOT_BACKGROUND_CHILD") == "1" {
		window, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		if window != 0 {
			os.Exit(2)
		}
		fmt.Print("background-stream-ok\n")
		os.Exit(0)
	}
	cmd := BackgroundCommand(os.Args[0], "-test.run=^TestBackgroundCommandHasNoConsoleAndKeepsOutput$")
	cmd.Env = append(os.Environ(), "ROBOT_BACKGROUND_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "background-stream-ok\n" {
		t.Fatalf("child output=%q err=%v", out, err)
	}
}
