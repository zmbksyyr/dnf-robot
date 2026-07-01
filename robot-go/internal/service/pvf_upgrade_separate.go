package service

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const upgradeSeparatePath = "etc/upgrade_separate.etc"
const upgradeSeparateLabel = "[separate upgrade max]"

type PVFUpgradeSeparateStatus struct {
	Path       string `json:"path"`
	Value      int    `json:"value"`
	OK         bool   `json:"ok"`
	NeedsPatch bool   `json:"needs_patch"`
	Message    string `json:"message,omitempty"`
}

type PVFUpgradeSeparatePatchResult struct {
	Path       string `json:"path"`
	BackupPath string `json:"backup_path,omitempty"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
	Patched    bool   `json:"patched"`
	OK         bool   `json:"ok"`
	Message    string `json:"message,omitempty"`
}

type pvfPatchEntry struct {
	treeOffset       int
	fileNameChecksum uint32
	name             string
	dataLen          uint32
	checksum         uint32
	offset           uint32
}

func InspectPVFUpgradeSeparate(path string) (PVFUpgradeSeparateStatus, error) {
	path = cleanPVFFilePath(path)
	status := PVFUpgradeSeparateStatus{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return status, err
	}
	data, strings, _, _, _, err := readPVFPlainFile(raw, upgradeSeparatePath)
	if err != nil {
		return status, err
	}
	value, _, err := findUpgradeSeparateValue(data, strings)
	if err != nil {
		return status, err
	}
	status.Value = value
	status.OK = value <= 7
	status.NeedsPatch = value > 7
	if status.OK {
		status.Message = "upgrade separate max is compatible"
	} else {
		status.Message = "upgrade separate max is above 7"
	}
	return status, nil
}

func PatchPVFUpgradeSeparate(path string, target int) (PVFUpgradeSeparatePatchResult, error) {
	path = cleanPVFFilePath(path)
	if target <= 0 {
		target = 7
	}
	result := PVFUpgradeSeparatePatchResult{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	data, strings, entry, tree, meta, err := readPVFPlainFile(raw, upgradeSeparatePath)
	if err != nil {
		return result, err
	}
	before, valueOffset, err := findUpgradeSeparateValue(data, strings)
	if err != nil {
		return result, err
	}
	result.Before = before
	result.After = before
	result.OK = before <= target

	backupPath := fmt.Sprintf("%s.bak_upgrade_separate_%s", path, time.Now().Format("20060102_150405"))
	if err := copyFile(path, backupPath); err != nil {
		return result, fmt.Errorf("backup pvf: %w", err)
	}
	result.BackupPath = backupPath

	if before <= target {
		result.Message = "no patch needed"
		return result, nil
	}

	binary.LittleEndian.PutUint32(data[valueOffset:valueOffset+4], uint32(target))
	alignedLen := align4(int(entry.dataLen))
	aligned := make([]byte, alignedLen)
	copy(aligned, data[:entry.dataLen])
	newChecksum := pvfDataChecksum(aligned, alignedLen, entry.fileNameChecksum)

	checksumOffset := entry.treeOffset + 12 + len(entry.name)
	binary.LittleEndian.PutUint32(tree[checksumOffset:checksumOffset+4], newChecksum)

	newTreeChecksum := pvfDataChecksum(tree, len(tree), meta.fileCount)
	binary.LittleEndian.PutUint32(raw[meta.treeChecksumOffset:meta.treeChecksumOffset+4], newTreeChecksum)
	encryptPVFBlockInto(raw[meta.treeStart:meta.treeEnd], tree, newTreeChecksum)

	encryptPVFBlockInto(raw[meta.dataStart+int(entry.offset):meta.dataStart+int(entry.offset)+alignedLen], aligned, newChecksum)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		return result, err
	}
	result.After = target
	result.Patched = true
	result.OK = true
	result.Message = "patched upgrade separate max"
	return result, nil
}

type pvfPatchMeta struct {
	fileCount          uint32
	treeChecksumOffset int
	treeStart          int
	treeEnd            int
	dataStart          int
}

func readPVFPlainFile(raw []byte, target string) ([]byte, []string, pvfPatchEntry, []byte, pvfPatchMeta, error) {
	if len(raw) < 56 {
		return nil, nil, pvfPatchEntry{}, nil, pvfPatchMeta{}, fmt.Errorf("pvf too small")
	}
	guidLen := int(binary.LittleEndian.Uint32(raw[0:4]))
	pos := 4 + guidLen
	if pos+16 > len(raw) {
		return nil, nil, pvfPatchEntry{}, nil, pvfPatchMeta{}, fmt.Errorf("pvf header truncated")
	}
	treeLen := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
	treeChecksum := binary.LittleEndian.Uint32(raw[pos+8 : pos+12])
	fileCount := binary.LittleEndian.Uint32(raw[pos+12 : pos+16])
	treeStart := pos + 16
	treeEnd := treeStart + treeLen
	if treeEnd > len(raw) {
		return nil, nil, pvfPatchEntry{}, nil, pvfPatchMeta{}, fmt.Errorf("pvf file tree truncated")
	}
	tree := append([]byte(nil), raw[treeStart:treeEnd]...)
	decryptPVFBlock(tree, treeLen, treeChecksum)
	meta := pvfPatchMeta{
		fileCount:          fileCount,
		treeChecksumOffset: pos + 8,
		treeStart:          treeStart,
		treeEnd:            treeEnd,
		dataStart:          treeEnd,
	}
	entries := parsePVFPatchEntries(tree, int(fileCount))
	var targetEntry *pvfPatchEntry
	var stringEntry *pvfPatchEntry
	for i := range entries {
		if entries[i].name == target {
			targetEntry = &entries[i]
		}
		if entries[i].name == "stringtable.bin" {
			stringEntry = &entries[i]
		}
	}
	if targetEntry == nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, fmt.Errorf("%s not found", target)
	}
	if stringEntry == nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, fmt.Errorf("stringtable.bin not found")
	}
	strings, err := readPVFStringTable(raw, meta.dataStart, *stringEntry)
	if err != nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, err
	}
	data, err := readPVFEntryData(raw, meta.dataStart, *targetEntry)
	if err != nil {
		return nil, nil, pvfPatchEntry{}, nil, meta, err
	}
	return data, strings, *targetEntry, tree, meta, nil
}

func parsePVFPatchEntries(tree []byte, fileCount int) []pvfPatchEntry {
	entries := make([]pvfPatchEntry, 0, fileCount)
	for offset, i := 0, 0; i < fileCount && offset+20 <= len(tree); i++ {
		nameLen := int(binary.LittleEndian.Uint32(tree[offset+4 : offset+8]))
		if nameLen < 0 || offset+20+nameLen > len(tree) {
			break
		}
		name := normalizePVFPath(string(tree[offset+8 : offset+8+nameLen]))
		entry := pvfPatchEntry{
			treeOffset:       offset,
			fileNameChecksum: binary.LittleEndian.Uint32(tree[offset : offset+4]),
			name:             name,
			dataLen:          binary.LittleEndian.Uint32(tree[offset+8+nameLen : offset+12+nameLen]),
			checksum:         binary.LittleEndian.Uint32(tree[offset+12+nameLen : offset+16+nameLen]),
			offset:           binary.LittleEndian.Uint32(tree[offset+16+nameLen : offset+20+nameLen]),
		}
		entries = append(entries, entry)
		offset += nameLen + 20
	}
	return entries
}

func readPVFEntryData(raw []byte, dataStart int, entry pvfPatchEntry) ([]byte, error) {
	aligned := align4(int(entry.dataLen))
	start := dataStart + int(entry.offset)
	end := start + aligned
	if start < 0 || end > len(raw) {
		return nil, fmt.Errorf("pvf data for %s truncated", entry.name)
	}
	data := append([]byte(nil), raw[start:end]...)
	decryptPVFBlock(data, aligned, entry.checksum)
	return data[:entry.dataLen], nil
}

func readPVFStringTable(raw []byte, dataStart int, entry pvfPatchEntry) ([]string, error) {
	data, err := readPVFEntryData(raw, dataStart, entry)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("stringtable.bin too small")
	}
	count := int(binary.LittleEndian.Uint32(data[0:4]))
	if count <= 0 || 4+count*4 > len(data) {
		return nil, fmt.Errorf("stringtable.bin invalid count")
	}
	out := make([]string, count)
	for i := 0; i < count; i++ {
		start := int(binary.LittleEndian.Uint32(data[4+i*4 : 8+i*4]))
		endOff := 8 + i*4
		if endOff+4 > len(data) {
			break
		}
		end := int(binary.LittleEndian.Uint32(data[endOff : endOff+4]))
		if start < 0 || end < start || end+4 > len(data) {
			continue
		}
		out[i] = cleanPVFTableString(decodePVFBytes(data[start+4 : end+4]))
	}
	return out, nil
}

func findUpgradeSeparateValue(data []byte, strings []string) (int, int, error) {
	for i := 2; i+10 <= len(data); i += 5 {
		if data[i] != 5 {
			continue
		}
		idx := int(binary.LittleEndian.Uint32(data[i+1 : i+5]))
		if idx < 0 || idx >= len(strings) || strings[idx] != upgradeSeparateLabel {
			continue
		}
		if data[i+5] != 2 {
			return 0, 0, fmt.Errorf("%s value has unexpected type %d", upgradeSeparateLabel, data[i+5])
		}
		return int(binary.LittleEndian.Uint32(data[i+6 : i+10])), i + 6, nil
	}
	return 0, 0, fmt.Errorf("%s value not found", upgradeSeparateLabel)
}

var pvfChecksumTable [256]uint32
var pvfChecksumReady bool

func pvfDataChecksum(buf []byte, dataLen int, seed uint32) uint32 {
	initPVFChecksumTable()
	if dataLen > len(buf) {
		dataLen = len(buf)
	}
	num := ^seed
	for index := 0; index+3 < dataLen; index += 4 {
		b0 := (buf[index] ^ byte(num)) & 0xff
		num3 := (num >> 8) ^ pvfChecksumTable[b0]
		b1 := (buf[index+1] ^ byte(num3)) & 0xff
		num5 := (num3 >> 8) ^ pvfChecksumTable[b1]
		b2 := (buf[index+2] ^ byte(num5)) & 0xff
		num7 := (num5 >> 8) ^ pvfChecksumTable[b2]
		b3 := (buf[index+3] ^ byte(num7)) & 0xff
		num = (num7 >> 8) ^ pvfChecksumTable[b3]
	}
	return ^num
}

func initPVFChecksumTable() {
	if pvfChecksumReady {
		return
	}
	current := uint32(1)
	for step := uint32(128); step > 0; step /= 2 {
		poly := uint32(0)
		if current&1 != 0 {
			poly = 0xEDB88320
		}
		current = (current >> 1) ^ poly
		for index, currentPos := uint32(0), step; index < 256; index += step * 2 {
			pvfChecksumTable[currentPos] = pvfChecksumTable[index] ^ current
			currentPos += step * 2
		}
	}
	pvfChecksumReady = true
}

func encryptPVFBlockInto(dst []byte, plain []byte, key uint32) {
	for i := 0; i+4 <= len(dst) && i+4 <= len(plain); i += 4 {
		v := binary.LittleEndian.Uint32(plain[i : i+4])
		v = (v << 6) | (v >> (32 - 6))
		binary.LittleEndian.PutUint32(dst[i:i+4], v^key^0x81A79011)
	}
}

func cleanPVFFilePath(path string) string {
	if path == "" {
		return path
	}
	return filepath.Clean(path)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
