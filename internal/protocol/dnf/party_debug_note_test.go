package dnf

import (
	"fmt"
	"testing"
)

// Mirrors the previous call-site pattern: the diagnostic note was formatted for
// every party packet even while no capture session was active.
func BenchmarkPartyDebugLegacySprintfInactive(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		note := fmt.Sprintf("type=153 anti=%t size=%d", false, 128)
		recordPartyDebugPacket(17000001, 0, "RX", "GAME", "REALTIME", "OBSERVED", note, nil)
	}
}

func BenchmarkPartyDebugNoteInactive(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		note := partyDebugNote("type=153 anti=%t size=%d", false, 128)
		recordPartyDebugPacket(17000001, 0, "RX", "GAME", "REALTIME", "OBSERVED", note, nil)
	}
}

// The guarded path must still format once a capture session is listening.
func BenchmarkPartyDebugNoteActive(b *testing.B) {
	if globalPartyDebug.active.Load() != nil {
		b.Skip("another capture session is active")
	}
	StartPartyDebug()
	b.Cleanup(func() { StopPartyDebug() })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		note := partyDebugNote("type=153 anti=%t size=%d index=%d", false, 128, i)
		if note == "" {
			b.Fatal("note was dropped while capturing")
		}
	}
}
