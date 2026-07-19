package webadmin

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

var defaultPartyCompatLayout = partyCompatLayout{
	site:                0x0864811a,
	cave:                0x08af75e4,
	rawSend:             0x086483e3,
	resumeSite:          0x08648124,
	getPacket:           0x0822b702,
	getPacketSignature:  []byte{0x55, 0x89, 0xe5, 0x83, 0xec, 0x28, 0x81, 0x7d, 0x0c, 0x17, 0x73, 0x01, 0x00, 0x7e, 0x46},
	resumeSiteSignature: []byte{0x89, 0xe0, 0x89, 0x85, 0x54, 0x8c, 0xfe, 0xff, 0x8d, 0x45, 0x9c, 0x89, 0x04, 0x24, 0xe8, 0x15, 0x5c, 0xf4, 0xff},
	rawSendSignature:    []byte{0x8d, 0x85, 0x64, 0x8c, 0xfe, 0xff, 0x89, 0x04, 0x24, 0xe8, 0xcf, 0x44, 0xf4, 0xff, 0xc7, 0x44, 0x24, 0x04, 0x00, 0x00, 0x00, 0x00},
}

var (
	partyCompatOriginalSite = []byte{0x80, 0x7d, 0xd3, 0x00, 0x0f, 0x84, 0xbf, 0x02, 0x00, 0x00}
	partyCompatZeroCave     = make([]byte, 128)
)

type partyCompatLayout struct {
	site                int64
	cave                int64
	rawSend             int64
	resumeSite          int64
	getPacket           int64
	getPacketSignature  []byte
	resumeSiteSignature []byte
	rawSendSignature    []byte
}

type memoryReadWriter interface {
	io.ReaderAt
	io.WriterAt
}

func inspectPartyCompatMemory(mem io.ReaderAt, layout partyCompatLayout) (bool, uint32, uint32, error) {
	if err := validatePartyCompatTarget(mem, layout); err != nil {
		return false, 0, 0, err
	}
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
		if allZero(cave) {
			return false, 0, 0, nil
		}
		start, end, ok := parsePartyCompatCave(layout, cave)
		if !ok {
			return false, 0, 0, fmt.Errorf("party compatibility code cave contains unknown data")
		}
		return false, start, end, nil
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
	if err := validatePartyCompatTarget(mem, layout); err != nil {
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
			if _, _, ok := parsePartyCompatCave(layout, caveBefore); !ok {
				return false, fmt.Errorf("party compatibility code cave is occupied")
			}
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

func validatePartyCompatTarget(mem io.ReaderAt, layout partyCompatLayout) error {
	targets := []struct {
		name      string
		address   int64
		signature []byte
	}{
		{name: "getPacket", address: layout.getPacket, signature: layout.getPacketSignature},
		{name: "resume site", address: layout.resumeSite, signature: layout.resumeSiteSignature},
		{name: "raw send", address: layout.rawSend, signature: layout.rawSendSignature},
	}
	for _, target := range targets {
		if len(target.signature) == 0 {
			continue
		}
		got, err := readMemory(mem, target.address, len(target.signature))
		if err != nil {
			return fmt.Errorf("read party compatibility %s signature: %w", target.name, err)
		}
		if !bytes.Equal(got, target.signature) {
			return fmt.Errorf("unsupported df_game_r: unexpected bytes at party compatibility %s: %x", target.name, got)
		}
	}
	return nil
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
	result = append(result, 0x0f, 0xb7, 0x40, 0x01, 0x66, 0x85, 0xc0)
	checkConnBranch := len(result)
	result = append(result, 0x74, 0, 0x66, 0x83, 0xf8, 0x06)
	belowPartyBranch := len(result)
	result = append(result, 0x0f, 0x82, 0, 0, 0, 0, 0x66, 0x83, 0xf8, 0x0b)
	partyRangeBranch := len(result)
	result = append(result, 0x0f, 0x86, 0, 0, 0, 0, 0x66, 0x83, 0xf8, 0x16)
	belowDungeonBranch := len(result)
	result = append(result, 0x0f, 0x82, 0, 0, 0, 0, 0x66, 0x83, 0xf8, 0x1f)
	dungeonRangeBranch := len(result)
	result = append(result, 0x0f, 0x86, 0, 0, 0, 0, 0x66, 0x3d, 0x99, 0x00)
	realtimeBranch := len(result)
	result = append(result, 0x74, 0)
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
	for _, branch := range []int{partyRangeBranch, dungeonRangeBranch} {
		patchInternalRelativeBranch(result, layout.cave, branch, rawOffset)
	}
	if err := patchInternalShortBranch(result, layout.cave, checkConnBranch, rawOffset); err != nil {
		return nil, err
	}
	if err := patchInternalShortBranch(result, layout.cave, realtimeBranch, rawOffset); err != nil {
		return nil, err
	}
	if len(result) > len(partyCompatZeroCave) {
		return nil, fmt.Errorf("party compatibility cave size is %d, max %d", len(result), len(partyCompatZeroCave))
	}
	result = append(result, make([]byte, len(partyCompatZeroCave)-len(result))...)
	return result, nil
}

func patchInternalShortBranch(code []byte, base int64, branchOffset, targetOffset int) error {
	next := base + int64(branchOffset+2)
	target := base + int64(targetOffset)
	delta := target - next
	if delta < -128 || delta > 127 {
		return fmt.Errorf("short branch target is out of range")
	}
	code[branchOffset+1] = byte(int8(delta))
	return nil
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
