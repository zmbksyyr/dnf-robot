//go:build linux

package marketapp

import (
	"fmt"
	"os/exec"
	"syscall"

	processfoundation "robot/internal/foundation/process"
)

func (a *App) startAuctionBeforeLoad(service marketServiceSpec) error {
	// The original loader rejects persisted level-85 listings before a running
	// service can be patched. Apply the same checked patches at the exec stop.
	cmd := exec.Command(service.bin, service.args...)
	cmd.Dir = service.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err := processfoundation.StartWithMemoryPatch(cmd, func(pid int, mem processfoundation.MemoryFile) error {
		result, err := patchAuctionProcessMemory(pid, mem)
		a.logAuctionMemoryPatchResult(result, false)
		if err != nil {
			return err
		}
		for _, entry := range result.Entries {
			if !entry.OK {
				return fmt.Errorf("auction startup patch %s: %s", entry.Name, entry.Message)
			}
		}
		return nil
	})
	if err == nil {
		go func() { _ = cmd.Wait() }()
	}
	return err
}
