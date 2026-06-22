package dnf

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
)

func MakeSkip(ptrn []byte) []int {
	pLen := len(ptrn)
	skip := make([]int, 256)
	for i := 0; i < 256; i++ {
		skip[i] = pLen
	}
	for i := 0; i < pLen; i++ {
		skip[ptrn[i]] = pLen - i - 1
		if skip[ptrn[i]] == 0 {
			skip[ptrn[i]] = pLen
		}
	}
	return skip
}

func MakeShift(ptrn []byte) []int {
	pLen := len(ptrn)
	shift := make([]int, pLen)
	sptr := pLen - 1
	c := ptrn[pLen-1]
	shift[sptr] = 1
	sptr--
	pptr := ptrn[pLen-2:]

	for sptr >= 0 {
		p1 := pLen - 2
		for p1 >= 0 && ptrn[p1] != c {
			p1--
		}
		p2 := pLen - 2
		p3 := p1
		for p3 >= 0 && p2 >= 0 && ptrn[p3] == ptrn[p2] && p2 >= len(ptrn)-len(pptr)-1 {
			p3--
			p2--
		}
		if p3 < 0 || p2 < len(ptrn)-len(pptr)-1 {
			shift[sptr] = pLen - sptr + p2 - p3
		} else {
			shift[sptr] = pLen - sptr + p2 - p3
		}
		sptr--
		if len(pptr) > 1 {
			pptr = pptr[:len(pptr)-1]
		}
	}
	return shift
}

func BMSearch(buf []byte, ptrn []byte, skip []int, shift []int) int {
	return bytes.Index(buf, ptrn)
}

func createBackup(filePath string) error {
	backupPath := filePath + ".tw_bak"
	if _, err := os.Stat(backupPath); err == nil {
		return nil
	}
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}
	PrintfBlue("\n[Patch] Backup created. Original preserved.\n")
	return ioutil.WriteFile(backupPath, data, 0644)
}

func writeFileReplace(filePath string, data []byte) error {
	info, err := os.Stat(filePath)
	mode := os.FileMode(0755)
	if err == nil {
		mode = info.Mode()
	}
	tmpPath := filePath + fmt.Sprintf(".tw_tmp_%d", os.Getpid())
	if err := ioutil.WriteFile(tmpPath, data, mode); err != nil {
		os.Remove(tmpPath)
		PrintfRed("\n[Patch] Target temp file write failed!\n")
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		PrintfRed("\n[Patch] Target file replace failed!\n")
		return err
	}
	return nil
}

func patchFile(filePath string, origSig []byte, patchBytes []byte, patchName string) error {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		PrintfRed("\n[Patch] Target file not found!\n")
		return err
	}
	if len(data) <= 0 {
		PrintfRed("\n[Patch] Target file too small!\n")
		return fmt.Errorf("file too small")
	}

	alreadyPatched := bytes.Index(data, patchBytes)
	if alreadyPatched != -1 {
		PrintfGreen("\n[Patch] Already patched - skipping.\n")
		return nil
	}

	ret := bytes.Index(data, origSig)
	if ret != -1 {
		if err := createBackup(filePath); err != nil {
			return err
		}
		copy(data[ret:], patchBytes)
		if err := writeFileReplace(filePath, data); err == nil {
			PrintfBlue("\n[Patch] Patch applied. Please restart the service.\n")
			return nil
		}
		return err
	}
	return nil
}

func replaceBytesOnce(filePath string, fromBytes []byte, toBytes []byte, patchName string) error {
	if len(fromBytes) != len(toBytes) {
		PrintfRed("\n[Patch] Invalid restore length.\n")
		return fmt.Errorf("invalid length")
	}
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		PrintfRed("\n[Patch] Target file not found!\n")
		return err
	}
	if len(data) <= 0 {
		PrintfRed("\n[Patch] Target file too small!\n")
		return fmt.Errorf("file too small")
	}

	ret := -1
	for i := 0; i <= len(data)-len(fromBytes); i++ {
		match := true
		for j := 0; j < len(fromBytes); j++ {
			if data[i+j] != fromBytes[j] {
				match = false
				break
			}
		}
		if match {
			ret = i
			break
		}
	}

	if ret == -1 {
		PrintfGreen("\n[Patch] Restore target not found - already clean.\n")
		return nil
	}

	if err := createBackup(filePath); err != nil {
		return err
	}
	copy(data[ret:], toBytes)
	if err := writeFileReplace(filePath, data); err == nil {
		fmt.Printf("\033[1;36m\n[Patch] Restore applied: %s. Please restart the service.\n\033[0m", patchName)
		return nil
	}
	return err
}

func RefishDZZ1(dfGamePath string) error {
	origSig := []byte{0x0A, 0x7F, 0x0B, 0x8B, 0x45, 0xF4, 0x0F, 0xB6, 0x40, 0x0D, 0x84, 0xC0, 0x79, 0x29, 0xC7, 0x44}
	patchBytes := []byte{0x0B, 0x7F, 0x0B, 0x8B, 0x45, 0xF4, 0x0F, 0xB6, 0x40, 0x0D, 0x84, 0xC0, 0x79, 0x29, 0xC7, 0x44}
	return patchFile(dfGamePath, origSig, patchBytes, "DZZ1")
}

func RefishDZZ2(dfGamePath string) error {
	origSig := []byte{0x3D, 0x00, 0xE9, 0x84, 0x01, 0x00, 0x00, 0x8B, 0x45, 0xF4, 0x0F, 0xB6, 0x40, 0x0D, 0x3C, 0x0A}
	patchBytes := []byte{0x3D, 0x00, 0xE9, 0x84, 0x01, 0x00, 0x00, 0x8B, 0x45, 0xF4, 0x0F, 0xB6, 0x40, 0x0D, 0x3C, 0x0B}
	return patchFile(dfGamePath, origSig, patchBytes, "DZZ2")
}

func ApplyZPD(dfGamePath string) error {
	origSig := []byte{0xE8, 0x8B, 0x1A, 0x3B, 0x00, 0x83, 0xF0, 0x01, 0x84, 0xC0, 0x74, 0x26}
	patchBytes := []byte{0xE8, 0x8B, 0x1A, 0x3B, 0x00, 0x83, 0xF0, 0x01, 0x84, 0xC0, 0x74, 0x00}
	return patchFile(dfGamePath, origSig, patchBytes, "ZPD")
}
