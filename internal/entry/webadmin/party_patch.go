package webadmin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultPartyCompatAccountStart uint32 = 17000000
	defaultPartyCompatAccountEnd   uint32 = 17001000
)

var defaultPartyCompatLayout = partyCompatLayout{
	site:       0x0864811a,
	cave:       0x08af75e4,
	rawSend:    0x086483e3,
	resumeSite: 0x08648124,
	getPacket:  0x0822b702,
}

var (
	partyCompatOriginalSite = []byte{0x80, 0x7d, 0xd3, 0x00, 0x0f, 0x84, 0xbf, 0x02, 0x00, 0x00}
	partyCompatZeroCave     = make([]byte, 128)
)

type partyCompatLayout struct {
	site       int64
	cave       int64
	rawSend    int64
	resumeSite int64
	getPacket  int64
}

type partyCompatConfig struct {
	AccountStart uint32 `json:"account_start"`
	AccountEnd   uint32 `json:"account_end"`
}

type partyCompatStatus struct {
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"`
	PID          int    `json:"pid,omitempty"`
	Port         int    `json:"port"`
	AccountStart uint32 `json:"account_start"`
	AccountEnd   uint32 `json:"account_end"`
	Message      string `json:"message,omitempty"`
}

type partyCompatRequest struct {
	Action       string `json:"action"`
	AccountStart uint32 `json:"account_start"`
	AccountEnd   uint32 `json:"account_end"`
}

type memoryReadWriter interface {
	io.ReaderAt
	io.WriterAt
}

func (s *Server) handlePartyCompat(w http.ResponseWriter, r *http.Request) {
	s.partyCompatMu.Lock()
	defer s.partyCompatMu.Unlock()

	cfg, err := s.loadPartyCompatConfig()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		status := inspectPartyCompat(s.cfg.RobotGamePort, cfg)
		writeJSON(w, map[string]interface{}{"ok": true, "result": status})
	case http.MethodPost:
		var req partyCompatRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		if err := validatePartyCompatRange(req.AccountStart, req.AccountEnd); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		enable := false
		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "on":
			enable = true
		case "off":
		default:
			writeJSON(w, map[string]interface{}{"ok": false, "error": "action must be on or off"})
			return
		}
		cfg = partyCompatConfig{AccountStart: req.AccountStart, AccountEnd: req.AccountEnd}
		status, err := setPartyCompat(s.cfg.RobotGamePort, cfg, enable)
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "result": status})
			return
		}
		if err := s.savePartyCompatConfig(cfg); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "result": status})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "result": status})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) partyCompatConfigPath() string {
	return filepath.Join(s.cfg.ConfigDir, "party_compat.json")
}

func (s *Server) loadPartyCompatConfig() (partyCompatConfig, error) {
	cfg := partyCompatConfig{AccountStart: defaultPartyCompatAccountStart, AccountEnd: defaultPartyCompatAccountEnd}
	data, err := os.ReadFile(s.partyCompatConfigPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("read party compatibility config: %w", err)
	}
	if err := validatePartyCompatRange(cfg.AccountStart, cfg.AccountEnd); err != nil {
		return cfg, fmt.Errorf("read party compatibility config: %w", err)
	}
	return cfg, nil
}

func (s *Server) savePartyCompatConfig(cfg partyCompatConfig) error {
	if err := os.MkdirAll(s.cfg.ConfigDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := s.partyCompatConfigPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func validatePartyCompatRange(start, end uint32) error {
	if start == 0 || end == 0 || start >= end {
		return fmt.Errorf("account range must be positive and start must be less than end")
	}
	return nil
}

func inspectPartyCompat(port int, cfg partyCompatConfig) partyCompatStatus {
	status := partyCompatStatus{State: "unavailable", Port: port, AccountStart: cfg.AccountStart, AccountEnd: cfg.AccountEnd}
	pid, err := gamePIDForPort(port)
	if err != nil {
		status.Message = err.Error()
		return status
	}
	status.PID = pid
	mem, err := os.Open(fmt.Sprintf("/proc/%d/mem", pid))
	if err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status
	}
	defer mem.Close()
	enabled, start, end, err := inspectPartyCompatMemory(mem, defaultPartyCompatLayout)
	if err != nil {
		status.State = "unknown"
		status.Message = err.Error()
		return status
	}
	status.Enabled = enabled
	if enabled {
		status.State = "on"
		status.AccountStart = start
		status.AccountEnd = end
	} else {
		status.State = "off"
	}
	return status
}

func setPartyCompat(port int, cfg partyCompatConfig, enable bool) (partyCompatStatus, error) {
	status := partyCompatStatus{Port: port, AccountStart: cfg.AccountStart, AccountEnd: cfg.AccountEnd}
	pid, err := gamePIDForPort(port)
	if err != nil {
		return status, err
	}
	status.PID = pid
	mem, err := os.OpenFile(fmt.Sprintf("/proc/%d/mem", pid), os.O_RDWR, 0)
	if err != nil {
		return status, err
	}
	defer mem.Close()

	if err := withStoppedProcess(pid, func() error {
		_, err := setPartyCompatMemory(mem, defaultPartyCompatLayout, cfg.AccountStart, cfg.AccountEnd, enable)
		return err
	}); err != nil {
		status.State = "error"
		status.Message = err.Error()
		return status, err
	}

	enabled, start, end, err := inspectPartyCompatMemory(mem, defaultPartyCompatLayout)
	if err != nil {
		return status, err
	}
	status.Enabled = enabled
	status.State = "off"
	if enabled {
		status.State = "on"
		status.AccountStart = start
		status.AccountEnd = end
	}
	if enabled != enable {
		return status, fmt.Errorf("party compatibility patch verification failed")
	}
	return status, nil
}

func gamePIDForPort(port int) (int, error) {
	data, err := exec.Command("ss", "-lntp").Output()
	if err != nil {
		return 0, fmt.Errorf("read listening ports: %w", err)
	}
	pid, err := parseGamePIDForPort(data, port)
	if err != nil {
		return 0, err
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return 0, err
	}
	for _, part := range strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00") {
		if filepath.Base(part) == "df_game_r" {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("pid %d on port %d is not df_game_r", pid, port)
}

func parseGamePIDForPort(data []byte, port int) (int, error) {
	portPattern := regexp.MustCompile(`:` + regexp.QuoteMeta(strconv.Itoa(port)) + `\s`)
	pidPattern := regexp.MustCompile(`pid=([0-9]+)`)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if !portPattern.Match(line) {
			continue
		}
		match := pidPattern.FindSubmatch(line)
		if len(match) != 2 {
			continue
		}
		pid, err := strconv.Atoi(string(match[1]))
		if err == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("df_game_r is not listening on port %d", port)
}

func inspectPartyCompatMemory(mem io.ReaderAt, layout partyCompatLayout) (bool, uint32, uint32, error) {
	site, err := readMemory(mem, layout.site, len(partyCompatOriginalSite))
	if err != nil {
		return false, 0, 0, err
	}
	cave, err := readMemory(mem, layout.cave, len(partyCompatZeroCave))
	if err != nil {
		return false, 0, 0, err
	}
	patchedSite, err := buildPartyCompatSite(layout)
	if err != nil {
		return false, 0, 0, err
	}
	switch {
	case bytes.Equal(site, partyCompatOriginalSite):
		if !allZero(cave) {
			if _, _, ok := parsePartyCompatCave(layout, cave); !ok {
				return false, 0, 0, fmt.Errorf("party compatibility code cave contains unknown data")
			}
		}
		return false, 0, 0, nil
	case bytes.Equal(site, patchedSite):
		start, end, ok := parsePartyCompatCave(layout, cave)
		if !ok {
			return false, 0, 0, fmt.Errorf("party compatibility site is patched but code cave is invalid")
		}
		return true, start, end, nil
	default:
		return false, 0, 0, fmt.Errorf("unexpected bytes at party compatibility patch site: %x", site)
	}
}

func setPartyCompatMemory(mem memoryReadWriter, layout partyCompatLayout, start, end uint32, enable bool) (bool, error) {
	if err := validatePartyCompatRange(start, end); err != nil {
		return false, err
	}
	siteBefore, err := readMemory(mem, layout.site, len(partyCompatOriginalSite))
	if err != nil {
		return false, err
	}
	caveBefore, err := readMemory(mem, layout.cave, len(partyCompatZeroCave))
	if err != nil {
		return false, err
	}
	patchedSite, err := buildPartyCompatSite(layout)
	if err != nil {
		return false, err
	}

	if enable {
		if !bytes.Equal(siteBefore, partyCompatOriginalSite) && !bytes.Equal(siteBefore, patchedSite) {
			return false, fmt.Errorf("unexpected bytes at party compatibility patch site: %x", siteBefore)
		}
		if bytes.Equal(siteBefore, partyCompatOriginalSite) && !allZero(caveBefore) {
			return false, fmt.Errorf("party compatibility code cave is occupied")
		}
		if bytes.Equal(siteBefore, patchedSite) {
			if _, _, ok := parsePartyCompatCave(layout, caveBefore); !ok {
				return false, fmt.Errorf("party compatibility code cave is invalid")
			}
		}
		desiredCave, err := buildPartyCompatCave(layout, start, end)
		if err != nil {
			return false, err
		}
		changed := !bytes.Equal(caveBefore, desiredCave) || !bytes.Equal(siteBefore, patchedSite)
		if !changed {
			return false, nil
		}
		if err := writeMemoryVerified(mem, layout.cave, desiredCave); err != nil {
			_ = writeMemoryVerified(mem, layout.cave, caveBefore)
			return false, err
		}
		if err := writeMemoryVerified(mem, layout.site, patchedSite); err != nil {
			_ = writeMemoryVerified(mem, layout.site, siteBefore)
			_ = writeMemoryVerified(mem, layout.cave, caveBefore)
			return false, err
		}
		return true, nil
	}

	if bytes.Equal(siteBefore, partyCompatOriginalSite) {
		if allZero(caveBefore) {
			return false, nil
		}
		if _, _, ok := parsePartyCompatCave(layout, caveBefore); !ok {
			return false, fmt.Errorf("party compatibility code cave contains unknown data")
		}
		return true, writeMemoryVerified(mem, layout.cave, partyCompatZeroCave)
	}
	if !bytes.Equal(siteBefore, patchedSite) {
		return false, fmt.Errorf("unexpected bytes at party compatibility patch site: %x", siteBefore)
	}
	if _, _, ok := parsePartyCompatCave(layout, caveBefore); !ok {
		return false, fmt.Errorf("party compatibility code cave is invalid")
	}
	if err := writeMemoryVerified(mem, layout.site, partyCompatOriginalSite); err != nil {
		return false, err
	}
	if err := writeMemoryVerified(mem, layout.cave, partyCompatZeroCave); err != nil {
		return true, fmt.Errorf("patch disabled but code cave cleanup failed: %w", err)
	}
	return true, nil
}

func buildPartyCompatSite(layout partyCompatLayout) ([]byte, error) {
	result := make([]byte, 0, len(partyCompatOriginalSite))
	var err error
	result, err = appendRelativeBranch(result, layout.site, []byte{0xe9}, layout.cave)
	if err != nil {
		return nil, err
	}
	for len(result) < len(partyCompatOriginalSite) {
		result = append(result, 0x90)
	}
	return result, nil
}

func buildPartyCompatCave(layout partyCompatLayout, start, end uint32) ([]byte, error) {
	if err := validatePartyCompatRange(start, end); err != nil {
		return nil, err
	}
	result := []byte{0x8b, 0x45, 0x08, 0x8b, 0x80, 0xac, 0x04, 0x07, 0x00, 0x3d}
	result = binary.LittleEndian.AppendUint32(result, start)
	accountLowBranch := len(result)
	result = append(result, 0x0f, 0x82, 0, 0, 0, 0, 0x3d)
	result = binary.LittleEndian.AppendUint32(result, end)
	accountHighBranch := len(result)
	result = append(result, 0x0f, 0x83, 0, 0, 0, 0)
	result = append(result, 0xc7, 0x44, 0x24, 0x04, 0, 0, 0, 0, 0x8b, 0x45, 0x0c, 0x89, 0x04, 0x24)
	var err error
	result, err = appendRelativeBranch(result, layout.cave, []byte{0xe8}, layout.getPacket)
	if err != nil {
		return nil, err
	}
	result = append(result, 0x0f, 0xb7, 0x40, 0x01, 0x66, 0x83, 0xf8, 0x06)
	belowPartyBranch := len(result)
	result = append(result, 0x0f, 0x82, 0, 0, 0, 0, 0x66, 0x83, 0xf8, 0x0b)
	partyRangeBranch := len(result)
	result = append(result, 0x0f, 0x86, 0, 0, 0, 0, 0x66, 0x83, 0xf8, 0x16)
	belowDungeonBranch := len(result)
	result = append(result, 0x0f, 0x82, 0, 0, 0, 0, 0x66, 0x83, 0xf8, 0x1f)
	dungeonRangeBranch := len(result)
	result = append(result, 0x0f, 0x86, 0, 0, 0, 0, 0x66, 0x3d, 0x99, 0x00)
	realtimeBranch := len(result)
	result = append(result, 0x0f, 0x84, 0, 0, 0, 0)
	fallbackOffset := len(result)
	result = append(result, 0x80, 0x7d, 0xd3, 0x00)
	result, err = appendRelativeBranch(result, layout.cave, []byte{0x0f, 0x84}, layout.rawSend)
	if err != nil {
		return nil, err
	}
	result, err = appendRelativeBranch(result, layout.cave, []byte{0xe9}, layout.resumeSite)
	if err != nil {
		return nil, err
	}
	rawOffset := len(result)
	result, err = appendRelativeBranch(result, layout.cave, []byte{0xe9}, layout.rawSend)
	if err != nil {
		return nil, err
	}
	for _, branch := range []int{accountLowBranch, accountHighBranch, belowPartyBranch, belowDungeonBranch} {
		patchInternalRelativeBranch(result, layout.cave, branch, fallbackOffset)
	}
	for _, branch := range []int{partyRangeBranch, dungeonRangeBranch, realtimeBranch} {
		patchInternalRelativeBranch(result, layout.cave, branch, rawOffset)
	}
	if len(result) > len(partyCompatZeroCave) {
		return nil, fmt.Errorf("party compatibility cave size is %d, max %d", len(result), len(partyCompatZeroCave))
	}
	result = append(result, make([]byte, len(partyCompatZeroCave)-len(result))...)
	return result, nil
}

func patchInternalRelativeBranch(code []byte, base int64, branchOffset, targetOffset int) {
	next := base + int64(branchOffset+6)
	target := base + int64(targetOffset)
	binary.LittleEndian.PutUint32(code[branchOffset+2:branchOffset+6], uint32(int32(target-next)))
}

func appendRelativeBranch(dst []byte, base int64, opcode []byte, target int64) ([]byte, error) {
	next := base + int64(len(dst)+len(opcode)+4)
	delta := target - next
	if delta < math.MinInt32 || delta > math.MaxInt32 {
		return nil, fmt.Errorf("relative branch target is out of range")
	}
	dst = append(dst, opcode...)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(int32(delta)))
	return dst, nil
}

func parsePartyCompatCave(layout partyCompatLayout, cave []byte) (uint32, uint32, bool) {
	if len(cave) != len(partyCompatZeroCave) {
		return 0, 0, false
	}
	start := binary.LittleEndian.Uint32(cave[10:14])
	end := binary.LittleEndian.Uint32(cave[21:25])
	want, err := buildPartyCompatCave(layout, start, end)
	if err != nil || !bytes.Equal(cave, want) {
		return 0, 0, false
	}
	return start, end, true
}

func readMemory(mem io.ReaderAt, address int64, size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := mem.ReadAt(buf, address)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n != size {
		return nil, fmt.Errorf("short memory read at 0x%x: %d/%d", address, n, size)
	}
	return buf, nil
}

func writeMemoryVerified(mem memoryReadWriter, address int64, value []byte) error {
	n, err := mem.WriteAt(value, address)
	if err != nil {
		return err
	}
	if n != len(value) {
		return fmt.Errorf("short memory write at 0x%x: %d/%d", address, n, len(value))
	}
	got, err := readMemory(mem, address, len(value))
	if err != nil {
		return err
	}
	if !bytes.Equal(got, value) {
		return fmt.Errorf("memory verification failed at 0x%x", address)
	}
	return nil
}

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}
