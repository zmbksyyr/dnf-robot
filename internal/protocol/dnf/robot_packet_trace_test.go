package dnf

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPacketTraceLaunchEnvironment(t *testing.T) {
	if os.Getenv("DNF_TEST_PACKET_TRACE_CHILD") == "1" {
		vo := newBufferedTestRobot()
		vo.Cipher = newPartyTestCipher(t)
		for _, typ := range []uint16{500, 561, 29} {
			packet := []byte{1, 0, 0, 7, 0, 0, 0}
			binary.LittleEndian.PutUint16(packet[1:3], typ)
			vo.parsePacket(packet)
		}
		return
	}
	for flags := 0; flags < 8; flags++ {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestPacketTraceLaunchEnvironment$")
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if strings.EqualFold(key, "DNF_TRACE_RX") || strings.EqualFold(key, "DNF_TRACE_RAW_ALL") || strings.EqualFold(key, "DNF_TRACE_RAW29") {
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env, "DNF_TEST_PACKET_TRACE_CHILD=1",
				fmt.Sprintf("DNF_TRACE_RX=%d", flags&1),
				fmt.Sprintf("DNF_TRACE_RAW_ALL=%d", (flags>>1)&1),
				fmt.Sprintf("DNF_TRACE_RAW29=%d", (flags>>2)&1))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("packet trace child: %v\n%s", err, output)
			}
			for _, check := range []struct {
				text string
				want bool
			}{
				{"[RX] typ=500 size=7", flags&1 != 0},
				{"[RX] typ=561 size=7", flags&1 != 0},
				{"[RX] typ=561 raw=01310207000000", flags&2 != 0},
				{"[RX] typ=29 size=7 raw=011d0007000000", flags&5 == 5},
			} {
				if got := strings.Contains(string(output), check.text); got != check.want {
					t.Errorf("trace %q present=%t, want %t\n%s", check.text, got, check.want, output)
				}
			}
		})
	}
}
