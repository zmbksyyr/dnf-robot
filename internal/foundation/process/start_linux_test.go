//go:build linux

package process

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestStartWithMemoryPatchBeforeChildRuns(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject=%t", reject), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "ran")
			cmd := exec.Command("/bin/sh", "-c", `printf started > "$1"`, "test-child", marker)
			rejected := errors.New("unsupported instructions")
			err := StartWithMemoryPatch(cmd, func(pid int, mem MemoryFile) error {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					return fmt.Errorf("child ran before patch: %v", err)
				}
				executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
				if err != nil {
					return err
				}
				maps, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
				if err != nil {
					return err
				}
				defer maps.Close()
				scan := bufio.NewScanner(maps)
				var address int64
				for scan.Scan() {
					fields := strings.Fields(scan.Text())
					if len(fields) >= 6 && fields[2] == "00000000" && fields[5] == executable {
						address, err = strconv.ParseInt(strings.SplitN(fields[0], "-", 2)[0], 16, 64)
						break
					}
				}
				if err != nil || address == 0 {
					return fmt.Errorf("child ELF mapping missing: %v", err)
				}
				var header [4]byte
				if _, err := mem.ReadAt(header[:], address); err != nil {
					return err
				}
				if string(header[:]) != "\x7fELF" {
					return fmt.Errorf("unexpected child ELF header %x", header)
				}
				if _, err := mem.WriteAt(header[:], address); err != nil {
					return err
				}
				if reject {
					return rejected
				}
				return nil
			})
			if reject {
				if !errors.Is(err, rejected) {
					t.Fatalf("start error=%v, want rejection", err)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("rejected child executed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(marker)
			if err != nil || string(content) != "started" {
				t.Fatalf("child did not resume: %q, %v", content, err)
			}
		})
	}
}
