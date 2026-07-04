package marketapp

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type auctionMemoryPatchSpec struct {
	name    string
	address int64
	expect  byte
	value   byte
}

var auctionMemoryPatchSpecs = []auctionMemoryPatchSpec{
	{name: "refine_average_price_valid", address: 0x0806523f, expect: 0x07, value: 0x7f},
	{name: "level_operate_parameter", address: 0x080811d7, expect: 0x46, value: 0x7f},
	{name: "refine_search_valid", address: 0x0808281f, expect: 0x07, value: 0x7f},
	{name: "level_specific", address: 0x0808292d, expect: 0x46, value: 0x7f},
	{name: "level_category_min", address: 0x08083472, expect: 0x46, value: 0x7f},
	{name: "level_category_max", address: 0x0808347d, expect: 0x46, value: 0x7f},
}

func (a *App) PatchAuctionMemory(AuctionMemoryPatchRequest) (AuctionMemoryPatchResult, error) {
	pid, err := pidOfAuction()
	if err != nil {
		return AuctionMemoryPatchResult{}, err
	}
	return a.patchAuctionMemoryPID(pid)
}

func (a *App) patchAuctionMemoryIfRunning(source string) {
	pid, err := pidOfAuction()
	if err != nil {
		a.appendLog(LogEvent{Type: "auction_memory_patch", Status: "skipped", Message: fmt.Sprintf("%s: %v", source, err)})
		return
	}
	a.mu.Lock()
	if a.auctionPatchPID == pid {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	result, err := a.patchAuctionMemoryPID(pid)
	if err != nil {
		a.appendLog(LogEvent{Type: "auction_memory_patch", Status: "failed", Message: fmt.Sprintf("%s pid=%d: %v", source, pid, err)})
		return
	}
	allOK := len(result.Entries) == len(auctionMemoryPatchSpecs)
	for _, entry := range result.Entries {
		if !entry.OK {
			allOK = false
			break
		}
	}
	if allOK {
		a.mu.Lock()
		a.auctionPatchPID = pid
		a.mu.Unlock()
	}
}

func (a *App) patchAuctionMemoryPID(pid int) (AuctionMemoryPatchResult, error) {
	result := AuctionMemoryPatchResult{
		PID:    pid,
		Target: fmt.Sprintf("/proc/%d/mem", pid),
	}
	mem, err := os.OpenFile(result.Target, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	defer mem.Close()

	for _, spec := range auctionMemoryPatchSpecs {
		entry := AuctionMemoryPatchEntry{
			Name:    spec.name,
			Address: fmt.Sprintf("0x%08x", spec.address),
			Expect:  spec.expect,
			Value:   spec.value,
		}
		var buf [1]byte
		if _, err := mem.ReadAt(buf[:], spec.address); err != nil {
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.Before = buf[0]
		if buf[0] != spec.value {
			if _, err := mem.WriteAt([]byte{spec.value}, spec.address); err != nil {
				entry.Message = err.Error()
				result.Entries = append(result.Entries, entry)
				continue
			}
			entry.Changed = true
			result.Patched++
		}
		if _, err := mem.ReadAt(buf[:], spec.address); err != nil {
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.After = buf[0]
		entry.OK = entry.After == spec.value
		if !entry.OK {
			entry.Message = "patch value not applied"
		}
		result.Entries = append(result.Entries, entry)
	}
	a.appendLog(LogEvent{Type: "auction_memory_patch", Status: "done", Message: fmt.Sprintf("pid=%d patched=%d", result.PID, result.Patched)})
	return result, nil
}

func pidOfAuction() (int, error) {
	out, err := exec.Command("pidof", "df_auction_r").Output()
	if err != nil {
		return 0, fmt.Errorf("df_auction_r pid not found: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("df_auction_r pid not found")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid df_auction_r pid %q", fields[0])
	}
	return pid, nil
}
