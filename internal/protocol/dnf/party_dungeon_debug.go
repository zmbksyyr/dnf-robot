package dnf

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Keep combat evidence separately from the heavily sampled route handshake.
// State changes prove what was requested; HP and death notifications prove what
// the peer actually reported. Neither is treated as a successful dungeon run.
func (r *RobotVo) recordPartyDungeonBodyUnsafe(peer uint32, direction string, body []byte) {
	if !partyDebugEnabled() || len(body) < 7 || (body[0] != 1 && body[0] != 2) {
		return
	}
	command := binary.LittleEndian.Uint16(body[1:3])
	if command == 4 || command == 39 && len(body) == 11 && binary.LittleEndian.Uint32(body[7:]) == 0x7e7e7e7e {
		return
	}
	recordPartyDebugPacket(r.UID, peer, direction, "DUNGEON", "DUNGEON_BODY", "OBSERVED",
		fmt.Sprintf("command=%d", command), body)
}

func partyDebugDungeonLines(started time.Time, events []partyDebugEvent) []string {
	const limit = 64
	var combat []partyDebugEvent
	var loading []partyDebugEvent
	for _, event := range events {
		if event.kind == "DUNGEON_BODY" {
			combat = append(combat, event)
		} else if event.kind == "DUNGEON_NOTIFY" {
			loading = append(loading, event)
		}
	}
	var lines []string
	if len(loading) > 0 {
		lines = append(lines, fmt.Sprintf("LOADING: observed game notifications and sends TOTAL=%d SHOWN=%d", len(loading), min(32, len(loading))))
		for _, event := range loading[max(0, len(loading)-32):] {
			lines = append(lines, partyDebugDungeonLine(started, event))
		}
	}
	if len(combat) == 0 {
		return lines
	}
	lines = append(lines, "COMBAT: observed peer bodies; cast requests do not establish damage or kills")
	if len(combat) > limit {
		lines = append(lines, fmt.Sprintf("COMBAT_OMITTED=%d", len(combat)-limit))
		// A quiet room can keep sending unrelated status traffic after its last
		// kill. Retain earlier damage/destroy evidence as well as the recent
		// traffic, so reporting later does not erase the result of the fight.
		var outcomes []partyDebugEvent
		for _, event := range combat[:len(combat)-limit] {
			if event.note == "command=20" || event.note == "command=23" {
				outcomes = append(outcomes, event)
			}
		}
		if len(outcomes) > 0 {
			lines = append(lines, fmt.Sprintf("COMBAT_EARLIER_OUTCOMES=%d SHOWN=%d", len(outcomes), min(limit, len(outcomes))))
			for _, event := range outcomes[max(0, len(outcomes)-limit):] {
				lines = append(lines, partyDebugDungeonLine(started, event))
			}
		}
		combat = combat[len(combat)-limit:]
	}
	for _, event := range combat {
		lines = append(lines, partyDebugDungeonLine(started, event))
	}
	return lines
}

func partyDebugDungeonLine(started time.Time, event partyDebugEvent) string {
	return fmt.Sprintf("+%d R%d>P%d %s %s %s", event.at.Sub(started).Milliseconds(),
		event.uid, event.peer, event.direction, event.note, partyDebugReportRaw(event))
}
