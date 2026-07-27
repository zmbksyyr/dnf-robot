package marketapp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type auctionMemoryPatchSpec struct {
	name         string
	fallbackAddr int64
	expect       byte
	alternates   []byte
	value        byte
	pattern      []byte
	targetOffset int
}

var auctionMemoryPatchSpecs = []auctionMemoryPatchSpec{
	{name: "refine_average_price_valid", fallbackAddr: 0x0806523f, expect: 0x07, value: 0x7f, targetOffset: 8, pattern: []byte{0x45, 0x0c, 0x88, 0x45, 0xfc, 0x80, 0x7d, 0xfc, 0x00, 0x76, 0x07, 0xb8, 0x00, 0x00, 0x00, 0x00}},
	{name: "level_operate_parameter", fallbackAddr: 0x080811d7, expect: 0x46, alternates: []byte{0x55}, value: 0x7f, targetOffset: 8, pattern: []byte{0x8b, 0x45, 0x20, 0x8b, 0x40, 0x14, 0x83, 0xf8, 0x00, 0x7e, 0x0a, 0xb8, 0x1b, 0x00, 0x00, 0x00}},
	{name: "refine_search_valid", fallbackAddr: 0x0808281f, expect: 0x07, value: 0x7f, targetOffset: 8, pattern: []byte{0x45, 0x0c, 0x88, 0x45, 0xfc, 0x80, 0x7d, 0xfc, 0x00, 0x0f, 0x96, 0xc0, 0xc9, 0xc3, 0x90, 0x55}},
	{name: "level_specific", fallbackAddr: 0x0808292d, expect: 0x46, alternates: []byte{0x55}, value: 0x7f, targetOffset: 8, pattern: []byte{0x8b, 0x45, 0x0c, 0x8b, 0x40, 0x0c, 0x83, 0xf8, 0x00, 0x7f, 0x07, 0xb8, 0x01, 0x00, 0x00, 0x00}},
	{name: "level_category_min", fallbackAddr: 0x08083472, expect: 0x46, alternates: []byte{0x55}, value: 0x7f, targetOffset: 8, pattern: []byte{0x8b, 0x45, 0x0c, 0x0f, 0xb6, 0x40, 0x09, 0x3c, 0x00, 0x77, 0x0b, 0x8b, 0x45, 0x0c, 0x0f, 0xb6}},
	{name: "level_category_max", fallbackAddr: 0x0808347d, expect: 0x46, alternates: []byte{0x55}, value: 0x7f, targetOffset: 8, pattern: []byte{0x8b, 0x45, 0x0c, 0x0f, 0xb6, 0x40, 0x0a, 0x3c, 0x00, 0x76, 0x0a, 0xb8, 0x1b, 0x00, 0x00, 0x00}},
}

// InspectAuctionMemoryPatch reads the running auction process without changing it.
// It deliberately uses the same version-aware signatures as the writer so web
// diagnostics cannot drift back to a separate fixed-address implementation.
func InspectAuctionMemoryPatch() (AuctionMemoryPatchResult, error) {
	pid, err := pidOfAuction()
	if err != nil {
		return AuctionMemoryPatchResult{}, err
	}
	result := AuctionMemoryPatchResult{PID: pid, Target: fmt.Sprintf("/proc/%d/mem", pid)}
	mem, err := os.Open(result.Target)
	if err != nil {
		return result, err
	}
	defer mem.Close()
	segments, err := executableSegments(pid, "df_auction_r")
	if err != nil {
		return result, err
	}
	for _, spec := range auctionMemoryPatchSpecs {
		entry := AuctionMemoryPatchEntry{Name: spec.name, Expect: spec.expect, Value: spec.value}
		address, err := locateAuctionPatchAddress(mem, segments, spec)
		if err != nil {
			entry.Address = fmt.Sprintf("0x%08x", spec.fallbackAddr)
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.Address = fmt.Sprintf("0x%08x", address)
		var buf [1]byte
		if _, err := mem.ReadAt(buf[:], address); err != nil {
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.Before = buf[0]
		entry.After = buf[0]
		entry.OK = buf[0] == spec.value
		if !entry.OK {
			entry.Message = fmt.Sprintf("supported original byte 0x%02x is not patched", buf[0])
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func (a *App) PatchAuctionMemory(AuctionMemoryPatchRequest) (AuctionMemoryPatchResult, error) {
	result, err := a.patchAuctionMemory()
	if err != nil {
		return result, err
	}
	a.logAuctionMemoryPatchResult(result, true)
	return result, nil
}

func (a *App) patchAuctionMemory() (AuctionMemoryPatchResult, error) {
	pid, err := pidOfAuction()
	if err != nil {
		return AuctionMemoryPatchResult{}, err
	}
	result := AuctionMemoryPatchResult{
		PID:    pid,
		Target: fmt.Sprintf("/proc/%d/mem", pid),
	}
	mem, err := os.OpenFile(result.Target, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	defer mem.Close()
	segments, err := executableSegments(pid, "df_auction_r")
	if err != nil {
		return result, err
	}

	for _, spec := range auctionMemoryPatchSpecs {
		entry := AuctionMemoryPatchEntry{
			Name:   spec.name,
			Expect: spec.expect,
			Value:  spec.value,
		}
		address, err := locateAuctionPatchAddress(mem, segments, spec)
		if err != nil {
			entry.Address = fmt.Sprintf("0x%08x", spec.fallbackAddr)
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.Address = fmt.Sprintf("0x%08x", address)
		var buf [1]byte
		if _, err := mem.ReadAt(buf[:], address); err != nil {
			entry.Message = err.Error()
			result.Entries = append(result.Entries, entry)
			continue
		}
		entry.Before = buf[0]
		switch {
		case buf[0] == spec.value:
		case isSupportedAuctionPatchOriginal(spec, buf[0]):
			if _, err := mem.WriteAt([]byte{spec.value}, address); err != nil {
				entry.Message = err.Error()
				result.Entries = append(result.Entries, entry)
				continue
			}
			entry.Changed = true
			result.Patched++
		default:
			entry.Message = fmt.Sprintf("unexpected byte 0x%02x at %s", buf[0], entry.Address)
			result.Entries = append(result.Entries, entry)
			continue
		}
		if _, err := mem.ReadAt(buf[:], address); err != nil {
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
	return result, nil
}

func (a *App) logAuctionMemoryPatchResult(result AuctionMemoryPatchResult, manual bool) {
	status := marketLogStatusSuccess
	failed := 0
	for _, entry := range result.Entries {
		if !entry.OK {
			failed++
		}
	}
	if failed > 0 {
		status = marketLogStatusFailed
	}
	if !manual && result.Patched == 0 && failed == 0 {
		status = marketLogStatusActive
	}
	a.appendLog(LogEvent{Type: "auction_memory_patch", Status: status, Message: fmt.Sprintf("pid=%d patched=%d failed=%d", result.PID, result.Patched, failed)})
}

type memorySegment struct {
	start int64
	end   int64
}

func executableSegments(pid int, name string) ([]memorySegment, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	segments := make([]memorySegment, 0)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.Contains(fields[1], "x") {
			continue
		}
		if name != "" && !strings.Contains(line, name) {
			continue
		}
		parts := strings.SplitN(fields[0], "-", 2)
		if len(parts) != 2 {
			continue
		}
		start, err1 := strconv.ParseInt(parts[0], 16, 64)
		end, err2 := strconv.ParseInt(parts[1], 16, 64)
		if err1 == nil && err2 == nil && end > start {
			segments = append(segments, memorySegment{start: start, end: end})
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("no executable %s segment found", name)
	}
	return segments, nil
}

func locateAuctionPatchAddress(mem *os.File, segments []memorySegment, spec auctionMemoryPatchSpec) (int64, error) {
	if len(spec.pattern) == 0 || spec.targetOffset < 0 || spec.targetOffset >= len(spec.pattern) {
		return 0, fmt.Errorf("%s has invalid pattern", spec.name)
	}
	matches := make([]int64, 0, 1)
	for _, segment := range segments {
		size := segment.end - segment.start
		if size <= 0 || size > 64*1024*1024 {
			continue
		}
		buf := make([]byte, size)
		n, err := mem.ReadAt(buf, segment.start)
		if err != nil && err != io.EOF {
			return 0, err
		}
		buf = buf[:n]
		for off := 0; off+len(spec.pattern) <= len(buf); off++ {
			if patchPatternMatch(buf[off:off+len(spec.pattern)], spec) {
				matches = append(matches, segment.start+int64(off+spec.targetOffset))
				if len(matches) > 1 {
					return 0, fmt.Errorf("%s pattern matched multiple locations", spec.name)
				}
			}
		}
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("%s pattern not found", spec.name)
	}
	return matches[0], nil
}

func patchPatternMatch(window []byte, spec auctionMemoryPatchSpec) bool {
	if len(window) != len(spec.pattern) {
		return false
	}
	if window[spec.targetOffset] != spec.value && !isSupportedAuctionPatchOriginal(spec, window[spec.targetOffset]) {
		return false
	}
	return bytes.Equal(window[:spec.targetOffset], spec.pattern[:spec.targetOffset]) &&
		bytes.Equal(window[spec.targetOffset+1:], spec.pattern[spec.targetOffset+1:])
}

func isSupportedAuctionPatchOriginal(spec auctionMemoryPatchSpec, value byte) bool {
	if value == spec.expect {
		return true
	}
	for _, alternate := range spec.alternates {
		if value == alternate {
			return true
		}
	}
	return false
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
