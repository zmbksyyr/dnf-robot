package monitor

import (
	"encoding/binary"
	"testing"
)

func TestBuildMegaphonePacket(t *testing.T) {
	packet, err := BuildAnnouncementPacket(KindMegaphone, "hello", "robot", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(packet[0:2]); got != opMegaphone {
		t.Fatalf("op got %#x want %#x", got, opMegaphone)
	}
	if got := int(binary.LittleEndian.Uint16(packet[2:4])); got != len(packet) {
		t.Fatalf("size got %d want %d", got, len(packet))
	}
	if packet[0x0b] != 11 || binary.LittleEndian.Uint16(packet[0x0c:0x0e]) != 7 || packet[0x2d] != 5 {
		t.Fatalf("unexpected megaphone payload")
	}
}

func TestBuildWebNoticePacket(t *testing.T) {
	web, err := BuildAnnouncementPacket(KindWebNoticeSingle, "web", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(web) != 0x0f {
		t.Fatalf("web size got %d want %d", len(web), 0x0f)
	}
	if got := binary.LittleEndian.Uint16(web[0:2]); got != opWebNoticeSingle {
		t.Fatalf("web op got %#x want %#x", got, opWebNoticeSingle)
	}
	if web[0x0a] != 3 || string(web[0x0b:0x0e]) != "web" || web[0x0e] != 0 {
		t.Fatalf("unexpected web payload")
	}
}
