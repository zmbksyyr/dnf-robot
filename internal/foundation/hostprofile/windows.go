//go:build windows

package hostprofile

import (
	"crypto/sha256"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	foundationlog "robot/internal/foundation/log"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var queryImage = kernel.NewProc("QueryFullProcessImageNameW")
var openEvent = kernel.NewProc("OpenEventW")
var setEvent = kernel.NewProc("SetEvent")

var verifiedImages sync.Map // image path -> verified file identity and header

type verifiedImage struct {
	size     int64
	modified time.Time
	hash     string
	address  uint64
	header   [64]byte
}

func WindowsSupported() bool {
	p := Current()
	for _, name := range []string{"game", "auction", "point"} {
		s, ok := p.Services[name]
		if !ok || s.PIDFile == "" || s.Executable == "" || s.Image == "" ||
			len(s.ImageSHA256) != 64 || len(s.Start) == 0 || s.Directory == "" || s.StopEvent == "" {
			return false
		}
	}
	return true
}

// Scope identity to an instance PID marker and its configured Windows host.
func ProcessID(name string) (int, error) {
	s, ok := ServiceFor(name)
	if !ok {
		return 0, fmt.Errorf("host service %q missing", name)
	}
	data, err := os.ReadFile(s.PIDFile)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid %s PID marker", name)
	}
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	// ERROR_INVALID_PARAMETER (87) is returned when the marker refers to an
	// exited process.  Treat that as stopped; all other errors remain visible.
	if err == syscall.Errno(87) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err = syscall.GetExitCodeProcess(h, &code); err != nil {
		return 0, err
	}
	if code != 259 {
		return 0, nil
	}
	var buf [32768]uint16
	size := uint32(len(buf))
	ok1, _, e := queryImage.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok1 == 0 {
		return 0, e
	}
	expected, err := filepath.Abs(s.Executable)
	if err != nil {
		return 0, err
	}
	if !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(buf[:size])), filepath.Clean(expected)) {
		return 0, fmt.Errorf("%s PID belongs to another executable", name)
	}
	// This is an ELF host, so also prove the pinned ELF is mapped at its
	// declared address before accepting a PID left over from an earlier run.
	if s.ImageSHA256 != "" {
		if err = verifyMappedImage(pid, s); err != nil {
			return 0, err
		}
	}
	return pid, nil
}

func verifyMappedImage(pid int, s Service) error {
	info, err := os.Stat(s.Image)
	if err != nil {
		return err
	}
	cached, found := verifiedImages.Load(s.Image)
	var verified verifiedImage
	if found {
		verified = cached.(verifiedImage)
	}
	if !found || verified.size != info.Size() || !verified.modified.Equal(info.ModTime()) || verified.hash != s.ImageSHA256 {
		data, err := os.ReadFile(s.Image)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != s.ImageSHA256 {
			return fmt.Errorf("service ELF hash differs from profile")
		}
		image, err := elf.Open(s.Image)
		if err != nil {
			return err
		}
		defer image.Close()
		if image.Type != elf.ET_EXEC {
			return fmt.Errorf("relocatable ELF needs load-bias support")
		}
		verified = verifiedImage{size: info.Size(), modified: info.ModTime(), hash: s.ImageSHA256}
		for _, p := range image.Progs {
			if p.Type == elf.PT_LOAD && p.Off == 0 && p.Filesz >= 64 && len(data) >= 64 {
				verified.address = p.Vaddr
				copy(verified.header[:], data[:64])
				break
			}
		}
		if verified.address == 0 {
			return fmt.Errorf("service ELF has no mapped header")
		}
		verifiedImages.Store(s.Image, verified)
	}
	// All supported DNF images keep their unmodified ELF header in PT_LOAD 0.
	h, err := syscall.OpenProcess(0x10, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	var header [64]byte
	var count uintptr
	read := kernel.NewProc("ReadProcessMemory")
	ok, _, e := read.Call(uintptr(h), uintptr(verified.address), uintptr(unsafe.Pointer(&header[0])), 64, uintptr(unsafe.Pointer(&count)))
	if ok == 0 || count != 64 {
		return fmt.Errorf("verify mapped ELF: %v", e)
	}
	if header != verified.header {
		return fmt.Errorf("PID mapped ELF differs from configured service")
	}
	return nil
}

func StopService(name string) error {
	pid, err := ProcessID(name)
	if err != nil || pid == 0 {
		return err
	}
	process, err := syscall.OpenProcess(0x100000, false, uint32(pid))
	if err == syscall.Errno(87) {
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(process)
	s, _ := ServiceFor(name)
	event := strings.ReplaceAll(s.StopEvent, "{pid}", strconv.Itoa(pid))
	if event == "" {
		return fmt.Errorf("%s has no stop event", name)
	}
	ptr, err := syscall.UTF16PtrFromString(event)
	if err != nil {
		return err
	}
	h, _, e := openEvent.Call(2, 0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		return fmt.Errorf("open %s stop event: %w", name, e)
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	ok, _, e := setEvent.Call(h)
	if ok == 0 {
		return e
	}
	foundationlog.Robotf("HOST_STOP_REQUEST service=%s pid=%d event=%s\n", name, pid, event)
	result, err := syscall.WaitForSingleObject(process, 120000)
	if err != nil {
		return err
	}
	if result == 0 {
		return nil
	}
	return fmt.Errorf("%s did not stop; process retained", name)
}

func StartService(name, logPath string) error {
	if pid, err := ProcessID(name); err != nil {
		return err
	} else if pid > 0 {
		return nil
	}
	s, _ := ServiceFor(name)
	if len(s.Start) == 0 {
		return fmt.Errorf("%s has no start command", name)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(s.Start[0], s.Start[1:]...)
	cmd.Dir = s.Directory
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x8}
	if err = cmd.Start(); err != nil {
		return err
	}
	if s.PIDFile != "" {
		if err := os.MkdirAll(filepath.Dir(s.PIDFile), 0755); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		if err := os.WriteFile(s.PIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	go func(pid int) {
		_ = cmd.Wait()
		if s.PIDFile == "" {
			return
		}
		data, err := os.ReadFile(s.PIDFile)
		if err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(pid) {
			_ = os.Remove(s.PIDFile)
		}
	}(cmd.Process.Pid)
	return nil
}

func ProcessStartTime(pid int) (time.Time, error) {
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return time.Time{}, err
	}
	defer syscall.CloseHandle(h)
	var created, exited, kernel, user syscall.Filetime
	if err = syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	// syscall.Filetime.Nanoseconds already converts FILETIME to Unix epoch.
	return time.Unix(0, created.Nanoseconds()), nil
}

func ImageSegments(pid int, name string) ([][2]int64, error) {
	service := "auction"
	if strings.Contains(name, "point") {
		service = "point"
	}
	actual, err := ProcessID(service)
	if err != nil {
		return nil, err
	}
	if actual != pid {
		return nil, fmt.Errorf("market process identity changed")
	}
	s, _ := ServiceFor(service)
	image, err := elf.Open(s.Image)
	if err != nil {
		return nil, err
	}
	defer image.Close()
	var result [][2]int64
	for _, p := range image.Progs {
		if p.Type == elf.PT_LOAD && p.Flags&elf.PF_X != 0 {
			result = append(result, [2]int64{int64(p.Vaddr), int64(p.Vaddr + p.Filesz)})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no executable market segments")
	}
	return result, nil
}
