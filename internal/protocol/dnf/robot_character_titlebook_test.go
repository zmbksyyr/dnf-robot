package dnf

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

// Preserved original Windows server/client exchange, 2026-11-27:
// state-packet-cycle1/tcp-decoded.private.jsonl, PID 14048. The specific
// category has titles 26652/26675 in slots 4/52, independently of progress.
const capturedTitlebookSpecific = "000000010000000200000004001c680000010000000000000000000000000000340033680000010000000000000000000000000000e5f7121a00000000000000"
const capturedTitlebookAchievements = "0200000088190000000000000000a7190000000000000000bf9a66d900000000"
const capturedTitlebookCompletion = "8819000001000000040000001c6800000000d28c44310000"

func titlebookTestCategory(category int, slots ...uint16) []byte {
	body := make([]byte, 11+len(slots)*characterTitlebookRecordSize)
	binary.LittleEndian.PutUint32(body[3:], uint32(category))
	binary.LittleEndian.PutUint32(body[7:], uint32(len(slots)))
	for i, slot := range slots {
		pos := 11 + i*characterTitlebookRecordSize
		binary.LittleEndian.PutUint16(body[pos:], slot)
		binary.LittleEndian.PutUint32(body[pos+2:], 26652)
	}
	return body
}

func titlebookTestPacket(t *testing.T, r *RobotVo, typ uint16, flag byte, body []byte, transport string) robotInboundPacket {
	t.Helper()
	raw := makePartyRecvPacket(typ, body)
	if transport == "encrypted" {
		padded := make([]byte, alignTo16(len(body)+4))
		copy(padded, body)
		raw = makeEncryptedPartyRecvPacket(t, r.Cipher, typ, padded)
	}
	raw[0] = flag
	return robotInboundPacket{typ: typ, flag: flag, size: len(raw), data: raw, isAnti: transport == "anti"}
}

func receiveTitlebookTestPacket(r *RobotVo, packet robotInboundPacket) {
	if packet.isAnti {
		r.handleCharacterTitlebookUnsafe(packet)
	} else {
		r.parsePacket(packet.data)
	}
}

func TestCharacterTitlebookCapturedInputs(t *testing.T) {
	category, snapshot, used, ok := parseCharacterTitlebookCategory(mustPartyHex(t, capturedTitlebookSpecific))
	if !ok || category != 1 || used != 53 || !snapshot.known || len(snapshot.slots) != 2 {
		t.Fatalf("captured titlebook category=%d used=%d snapshot=%+v ok=%t", category, used, snapshot, ok)
	}
	for slot, id := range map[uint16]uint32{4: 26652, 52: 26675} {
		record := snapshot.slots[slot]
		if binary.LittleEndian.Uint16(record[:2]) != slot || binary.LittleEndian.Uint32(record[2:]) != id || binary.LittleEndian.Uint32(record[6:]) != 1 {
			t.Fatalf("captured record %d = %x", slot, record)
		}
	}
	entries, used, ok := parseCharacterAchievements(mustPartyHex(t, capturedTitlebookAchievements))
	if !ok || used != 24 || len(entries) != 2 || entries[6536] != [3]uint16{} || entries[6567] != [3]uint16{} {
		t.Fatalf("captured achievements=%v used=%d ok=%t", entries, used, ok)
	}
	info := characterTitlebookInfo{achievements: map[uint32][3]uint16{6533: {499, 0, 0}, 6536: {}, 6567: {}}, achievementsKnown: true}
	info.categories[1] = snapshot
	categories, titles, achievements, completed := info.summary()
	if categories != 1 || titles != 2 || achievements != 3 || completed != 2 {
		t.Fatalf("remaining triggers treated as progress earned: %d/%d/%d/%d", categories, titles, achievements, completed)
	}
}

func TestCharacterTitlebookCategoryBoundsAndMalformedInput(t *testing.T) {
	for category, capacity := range characterTitlebookCapacities {
		if _, _, _, ok := parseCharacterTitlebookCategory(titlebookTestCategory(category, uint16(capacity-1))); !ok {
			t.Fatalf("last native slot rejected: category=%d", category)
		}
		if _, _, _, ok := parseCharacterTitlebookCategory(titlebookTestCategory(category, uint16(capacity))); ok {
			t.Fatalf("out-of-bounds slot accepted: category=%d", category)
		}
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"other user":        func(b []byte) []byte { b[1] = 1; return b },
		"other info type":   func(b []byte) []byte { b[0] = 1; return b },
		"negative category": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[3:], ^uint32(0)); return b },
		"extra category":    func(b []byte) []byte { b[3] = 4; return b },
		"negative count":    func(b []byte) []byte { binary.LittleEndian.PutUint32(b[7:], ^uint32(0)); return b },
		"excess count":      func(b []byte) []byte { binary.LittleEndian.PutUint32(b[7:], 201); return b },
		"empty item":        func(b []byte) []byte { clear(b[13:17]); return b },
		"duplicate slot":    func(b []byte) []byte { copy(b[32:34], b[11:13]); return b },
		"short record":      func(b []byte) []byte { return b[:len(b)-1] },
		"unexpected tail":   func(b []byte) []byte { return append(b, 1, 2, 3) },
		"nonzero padding":   func(b []byte) []byte { return append(b, 1, 2, 3, 4, 1) },
		"oversized tail":    func(b []byte) []byte { return append(b, make([]byte, 20)...) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, ok := parseCharacterTitlebookCategory(mutate(titlebookTestCategory(1, 4, 52))); ok {
				t.Fatal("accepted malformed or foreign category")
			}
		})
	}
	for length := 0; length < 32; length++ {
		if _, _, _, ok := parseCharacterTitlebookCategory(titlebookTestCategory(1, 4)[:length]); ok {
			t.Fatalf("accepted truncated titlebook at %d", length)
		}
	}
	for _, body := range [][]byte{nil, {1, 0, 0}, {1, 0, 0, 0}, {1, 2, 0, 0}, append([]byte{2, 0, 0, 0}, make([]byte, 20)...)} {
		if _, _, ok := parseCharacterAchievements(body); ok {
			t.Fatalf("accepted malformed achievements %x", body)
		}
	}
}

func TestCharacterTitlebookDispatchKnowledgeAndLifetime(t *testing.T) {
	for _, transport := range []string{"encrypted", "plain", "anti"} {
		t.Run(transport, func(t *testing.T) {
			r := NewRobotVo(nil)
			r.Cipher = newPartyTestCipher(t)
			r.State, r.characterUniqueID = StateLogin, 2
			r.characterResources = characterResourcePool{hp: 123, mp: 77, hpObserved: true, mpObserved: true, syncPending: true}
			send := func(typ uint16, flag byte, body []byte) {
				receiveTitlebookTestPacket(r, titlebookTestPacket(t, r, typ, flag, body, transport))
			}
			// A delta before the full list establishes only that entry.
			trigger := mustPartyHex(t, "0185190000f30100000000")
			send(characterAchievementTrigger, 1, trigger)
			if r.characterResources.titlebook.achievementsKnown || r.characterResources.titlebook.achievements[6533] != [3]uint16{499, 0, 0} {
				t.Fatal("partial progress became full collection knowledge")
			}
			for category := 0; category < 4; category++ {
				send(characterTitlebookList, 0, titlebookTestCategory(category))
			}
			if known, titles, _, _ := r.characterResources.titlebook.summary(); known != 4 || titles != 0 {
				t.Fatal("observed empty categories not retained")
			}
			send(characterTitlebookList, 0, mustPartyHex(t, capturedTitlebookSpecific)[:53])
			send(characterAchievementList, 0, mustPartyHex(t, capturedTitlebookAchievements)[:24])
			if info := &r.characterResources.titlebook; !info.achievementsKnown || len(info.achievements) != 2 || len(info.categories[1].slots) != 2 {
				t.Fatalf("full inputs missing: %+v", info)
			}
			// Foreign inspection and an incorrect flag cannot erase our list.
			foreign := titlebookTestCategory(1)
			foreign[1] = 2
			send(characterTitlebookList, 0, foreign)
			send(characterTitlebookList, 1, titlebookTestCategory(1))
			if len(r.characterResources.titlebook.categories[1].slots) != 2 {
				t.Fatal("foreign list replaced self snapshot")
			}
			// Completion has a title ID and endurance, not the complete 21-byte
			// item. Keep progress, invalidate the affected category.
			send(characterAchievementComplete, 0, mustPartyHex(t, capturedTitlebookCompletion)[:18])
			if r.characterResources.titlebook.categories[1].known || !r.characterResources.titlebook.achievementsKnown {
				t.Fatal("completion created a fabricated complete title list")
			}
			send(characterTitlebookList, 0, titlebookTestCategory(1))
			if !r.characterResources.titlebook.categories[1].known || len(r.characterResources.titlebook.categories[1].slots) != 0 {
				t.Fatal("fresh empty list did not replace previous records")
			}
			if p := r.characterResources; p.hp != 123 || p.mp != 77 || !p.hpObserved || !p.mpObserved || !p.syncPending || p.definitionHP != 0 || p.definitionMP != 0 {
				t.Fatal("collection input changed current resources or applied unverified rewards")
			}
			before := r.characterResources.titlebook
			r.initializeCharacterResourcesUnsafe()
			if !reflect.DeepEqual(before, r.characterResources.titlebook) {
				t.Fatal("attribute initialization discarded separately observed collection inputs")
			}
			r.State, r.characterUniqueID = StateRun, 0
			send(characterTitlebookList, 0, titlebookTestCategory(1, 4))
			if !reflect.DeepEqual(before, r.characterResources.titlebook) {
				t.Fatal("unknown selected identity accepted collection input")
			}
			r.characterUniqueID = 2
			for _, state := range []ClientState{StateStop, StateWrong, StateSelect} {
				r.State = state
				send(characterTitlebookList, 0, titlebookTestCategory(1, 4))
				if !reflect.DeepEqual(before, r.characterResources.titlebook) {
					t.Fatal("inactive session accepted collection input")
				}
			}
			r.State, r.ReturnSelectPending = StateRun, true
			send(characterTitlebookList, 0, titlebookTestCategory(1, 4))
			if !reflect.DeepEqual(before, r.characterResources.titlebook) {
				t.Fatal("pending return accepted late collection input")
			}
			r.finishReturnToSelectUnsafe()
			if known, _, n, _ := r.characterResources.titlebook.summary(); known != 0 || n != 0 || r.characterResources.titlebook.achievementsKnown {
				t.Fatal("character return retained collection knowledge")
			}
			r.characterResources.titlebook = before
			r.Load(UserLoginInfo{CID: 999})
			if known, _, n, _ := r.characterResources.titlebook.summary(); known != 0 || n != 0 {
				t.Fatal("login retained previous collection")
			}
			r.characterResources.titlebook = before
			r.CloseOut()
			if known, _, n, _ := r.characterResources.titlebook.summary(); known != 0 || n != 0 {
				t.Fatal("logout retained collection")
			}
		})
	}
}

func TestCharacterTitlebookAchievementStreamBoundAndFreshList(t *testing.T) {
	r := NewRobotVo(nil)
	r.Cipher = newPartyTestCipher(t)
	r.State, r.characterUniqueID = StateRun, 2
	full := make([]byte, 4+characterAchievementLimit*10)
	binary.LittleEndian.PutUint32(full, characterAchievementLimit)
	for i := 0; i < characterAchievementLimit; i++ {
		binary.LittleEndian.PutUint32(full[4+i*10:], uint32(i))
		binary.LittleEndian.PutUint16(full[8+i*10:], 1)
	}
	receiveTitlebookTestPacket(r, titlebookTestPacket(t, r, characterAchievementList, 0, full, "plain"))
	if info := r.characterResources.titlebook; !info.achievementsKnown || len(info.achievements) != characterAchievementLimit {
		t.Fatal("native maximum achievement snapshot rejected")
	}
	// A new entry beyond the native snapshot capacity invalidates certainty;
	// a later full snapshot must be able to restore it, including known empty.
	delta := make([]byte, 11)
	delta[0] = 1
	binary.LittleEndian.PutUint32(delta[1:], 1000)
	receiveTitlebookTestPacket(r, titlebookTestPacket(t, r, characterAchievementTrigger, 1, delta, "plain"))
	if info := r.characterResources.titlebook; info.achievementsKnown || len(info.achievements) != 0 {
		t.Fatal("overflowed incremental stream retained partial certainty")
	}
	receiveTitlebookTestPacket(r, titlebookTestPacket(t, r, characterAchievementList, 0, make([]byte, 4), "plain"))
	if info := r.characterResources.titlebook; !info.achievementsKnown || len(info.achievements) != 0 {
		t.Fatal("fresh empty achievement list did not restore knowledge")
	}
	for word := 0; word < 3; word++ {
		remaining := [3]uint16{}
		remaining[word] = 1
		info := characterTitlebookInfo{achievements: map[uint32][3]uint16{1: remaining}}
		if _, _, _, completed := info.summary(); completed != 0 {
			t.Fatalf("remaining trigger word %d treated as complete", word)
		}
	}
}

func TestCharacterTitlebookMovesInvalidateTheirOwnCategory(t *testing.T) {
	for _, typ := range []uint16{characterTitlebookPut, characterTitlebookGet} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			r := NewRobotVo(nil)
			r.Cipher = newPartyTestCipher(t)
			r.State, r.characterUniqueID = StateRun, 2
			for i := range r.characterResources.titlebook.categories {
				r.characterResources.titlebook.categories[i] = characterTitlebookCategory{known: true}
			}
			body := make([]byte, 17)
			body[0] = 1
			// Source/destination inventory coordinates deliberately differ from
			// the titlebook coordinates. PUT and GET reverse their order.
			if typ == characterTitlebookPut {
				body[1] = 9
				body[5] = 24
				body[9] = 2
				body[13] = 119
			} else {
				body[1] = 2
				body[5] = 119
				body[9] = 9
				body[13] = 24
			}
			receiveTitlebookTestPacket(r, titlebookTestPacket(t, r, typ, 1, body, "plain"))
			for i, c := range r.characterResources.titlebook.categories {
				if c.known != (i != 2) {
					t.Fatalf("wrong category invalidated: %d", i)
				}
			}
			r.characterResources.titlebook.categories[2].known = true
			body[0] = 0
			receiveTitlebookTestPacket(r, titlebookTestPacket(t, r, typ, 1, body, "plain"))
			if !r.characterResources.titlebook.categories[2].known {
				t.Fatal("failed title move erased authoritative snapshot")
			}
		})
	}
}
