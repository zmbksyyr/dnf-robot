package monitor

import (
	"encoding/binary"
	"testing"
)

func TestBuildNotifyNewMailPacket(t *testing.T) {
	packet := BuildNotifyNewMailPacket(123456)
	if len(packet) != 0x12 {
		t.Fatalf("packet length = %d, want 18", len(packet))
	}
	if got := binary.LittleEndian.Uint16(packet[0:2]); got != 0x0514 {
		t.Fatalf("opcode = %#x, want 0x0514", got)
	}
	if got := binary.LittleEndian.Uint16(packet[2:4]); got != 0x12 {
		t.Fatalf("size = %d, want 18", got)
	}
	if got := binary.LittleEndian.Uint32(packet[0x0a:0x0e]); got != 123456 {
		t.Fatalf("charac_no = %d, want 123456", got)
	}
	if got := binary.LittleEndian.Uint32(packet[0x0e:0x12]); got != 0 {
		t.Fatalf("channel id = %d, want monitor-filled zero", got)
	}
}
