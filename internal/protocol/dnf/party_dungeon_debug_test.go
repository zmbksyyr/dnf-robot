package dnf

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestDungeonDebugRetainsDamageBesideStateAndSkipsEmptyHeartbeat(t *testing.T) {
	StopPartyDebug()
	StartPartyDebug()
	t.Cleanup(func() { StopPartyDebug() })
	r := &RobotVo{UID: 17000002}
	heartbeat := []byte{2, 39, 0, 1, 0, 0, 0, 0x7e, 0x7e, 0x7e, 0x7e}
	for i := 0; i < 100; i++ {
		heartbeat[4] = byte(i)
		r.recordPartyDungeonBodyUnsafe(1, "RX", heartbeat)
	}
	state := buildPartyLegacySkillStateBody(7, 8, []byte{0}, 9)
	damage := dungeonDamageBody(t, dungeonDamageCaptures[0].plain)
	r.recordPartyDungeonBodyUnsafe(1, "TX", state)
	r.recordPartyDungeonBodyUnsafe(1, "RX", damage)
	result := StopPartyDebug()
	report := strings.Join(result.ReportLines, "\n")
	for _, want := range []string{"COMBAT:", "TX command=5", "RX command=20", base64.RawStdEncoding.EncodeToString(damage)} {
		if !strings.Contains(report, want) {
			t.Fatalf("missing combat evidence %q:\n%s", want, report)
		}
	}
	if result.EventCount != 2 || strings.Contains(report, "command=39") {
		t.Fatalf("empty heartbeats displaced evidence: %+v", result)
	}
}

func TestDungeonDebugReportBoundsRawCombatEvidence(t *testing.T) {
	now := time.Now()
	events := make([]partyDebugEvent, 100)
	for i := range events {
		events[i] = partyDebugEvent{at: now.Add(time.Duration(i) * time.Millisecond), kind: "DUNGEON_BODY", uid: 1, note: "command=5"}
	}
	lines := partyDebugDungeonLines(now, events)
	if len(lines) != 66 || lines[1] != "COMBAT_OMITTED=36" || !strings.HasPrefix(lines[2], "+36 ") {
		t.Fatalf("unbounded or misleading combat evidence: %v", lines)
	}
}

func TestDungeonDebugRetainsEarlierOutcomeAfterQuietRoomTraffic(t *testing.T) {
	now := time.Now()
	damage := dungeonDamageBody(t, dungeonDamageCaptures[2].plain)
	events := []partyDebugEvent{{at: now, kind: "DUNGEON_BODY", uid: 1, direction: "RX", note: "command=20", raw: damage}}
	for i := 0; i < 200; i++ {
		events = append(events, partyDebugEvent{at: now.Add(time.Duration(i+1) * time.Second), kind: "DUNGEON_BODY", uid: 1, note: "command=70"})
	}
	lines := partyDebugDungeonLines(now, events)
	if len(lines) != 68 || lines[2] != "COMBAT_EARLIER_OUTCOMES=1 SHOWN=1" || !strings.Contains(lines[3], base64.RawStdEncoding.EncodeToString(damage)) {
		t.Fatalf("quiet-room traffic displaced the earlier outcome: %v", lines)
	}
}

func TestDungeonDebugIncludesBoundedLoadingWithoutCombat(t *testing.T) {
	now := time.Now()
	var events []partyDebugEvent
	for i := 0; i < 40; i++ {
		events = append(events, partyDebugEvent{at: now.Add(time.Duration(i) * time.Millisecond), kind: "DUNGEON_NOTIFY", uid: 1, direction: "RX", note: "type=29", raw: []byte{0, 29, 0}})
	}
	lines := partyDebugDungeonLines(now, events)
	if len(lines) != 33 || !strings.Contains(lines[0], "TOTAL=40 SHOWN=32") || !strings.HasPrefix(lines[1], "+8 ") || !strings.Contains(lines[32], "RX type=29 B64[3]=AB0A") {
		t.Fatalf("loading evidence is missing or unbounded: %v", lines)
	}
}
