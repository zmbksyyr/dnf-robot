package dnf

import (
	"encoding/binary"
	"testing"
)

// Ordinary job0/level1 town login from the original Windows server. The
// compressed mode-0 record belongs to the SELECT_CHARACTER world unique 2.
const capturedCharacterLevelInfo = "789c636064606260676060284a2dcb4c2d4f64606064200a20296346164f8131fe0361c80ac989001b51070b00000000"

func TestCharacterLevelIdentityCapturedPrefixAndUnsignedByte(t *testing.T) {
	body, err := inflatePartyInfo(mustPartyHex(t, capturedCharacterLevelInfo))
	if err != nil {
		t.Fatal(err)
	}
	info, ok := parseInflatedCharacterLevelInfo(body, 2)
	if !ok || info != (characterLevelInfo{known: true, uniqueID: 2, level: 1}) {
		t.Fatalf("captured identity = %+v, ok=%t", info, ok)
	}
	for _, nameLength := range []int{0, 7, 30} {
		plain := make([]byte, 9+nameLength+3)
		binary.LittleEndian.PutUint16(plain[1:3], 1)
		binary.LittleEndian.PutUint16(plain[3:5], 2)
		binary.LittleEndian.PutUint32(plain[5:9], uint32(nameLength))
		for _, level := range []byte{0, 1, 2, 84, 85, 86, 127, 128, 255} {
			plain[len(plain)-1] = level
			info, ok := parseInflatedCharacterLevelInfo(plain, 2)
			if !ok || !info.known || info.level != level {
				t.Fatalf("name length=%d level=%d: %+v, ok=%t", nameLength, level, info, ok)
			}
		}
		for end := 0; end < len(plain); end++ {
			if _, ok := parseInflatedCharacterLevelInfo(plain[:end], 2); ok {
				t.Fatalf("accepted truncated identity: name=%d end=%d", nameLength, end)
			}
		}
	}
	for _, self := range []uint16{0, 1, 3} {
		if _, ok := parseInflatedCharacterLevelInfo(body, self); ok {
			t.Fatalf("accepted another/unknown character: %d", self)
		}
	}
	for _, offsetValues := range []struct {
		offset int
		values []uint32
		width  int
	}{
		{0, []uint32{1, 2, 3, 255}, 1},
		{1, []uint32{0, 2, 65535}, 2},
		{5, []uint32{31, 32, 0x80000000, 0xffffffff}, 4},
	} {
		for _, value := range offsetValues.values {
			other := append([]byte(nil), body...)
			switch offsetValues.width {
			case 1:
				other[offsetValues.offset] = byte(value)
			case 2:
				binary.LittleEndian.PutUint16(other[offsetValues.offset:], uint16(value))
			case 4:
				binary.LittleEndian.PutUint32(other[offsetValues.offset:], value)
			}
			if _, ok := parseInflatedCharacterLevelInfo(other, 2); ok {
				t.Fatalf("accepted unsupported identity field at %d = %d", offsetValues.offset, value)
			}
		}
	}
}

func TestCharacterLevelNotificationRequiresCompleteLayout(t *testing.T) {
	for size := 0; size <= 65; size++ {
		body := make([]byte, size)
		if size > 0 {
			body[0] = 255
		}
		level, ok := parseCharacterLevelNotification(body)
		want := size == 41 || size == 48
		if ok != want || ok && level != 255 {
			t.Fatalf("notification bytes=%d: level=%d ok=%t", size, level, ok)
		}
	}
	for i := 41; i < 48; i++ {
		body := make([]byte, 48)
		body[i] = 0xa7
		_, ok := parseCharacterLevelNotification(body)
		if ok != (i < 45) {
			t.Fatalf("trailer/padding byte %d: ok=%t", i, ok)
		}
	}
}

func TestCharacterLevelDispatchUpdatesAndSessionBoundaries(t *testing.T) {
	for _, transport := range []string{"plain", "encrypted", "anti"} {
		t.Run(transport, func(t *testing.T) {
			r := NewRobotVo(nil)
			r.Cipher = newPartyTestCipher(t)
			r.State, r.SelectCharacSent = StateLogin, true
			packet := func(typ uint16, flag byte, body []byte) robotInboundPacket {
				raw := makePartyRecvPacket(typ, body)
				if transport == "encrypted" {
					block := 16
					if typ == 37 {
						block = 8
					}
					padded := make([]byte, alignTo(len(body)+4, block))
					copy(padded, body)
					copy(padded[len(body):], []byte{0xa7, 0xb8, 0xc9, 0xda})
					raw = makeEncryptedPartyRecvPacket(t, r.Cipher, typ, padded)
				}
				raw[0] = flag
				return robotInboundPacket{typ: typ, flag: flag, data: raw, size: len(raw), isAnti: transport == "anti"}
			}
			deliver := func(p robotInboundPacket) {
				if p.isAnti {
					r.handleCharacterInfoUnsafe(p)
				} else {
					r.parsePacket(p.data)
				}
			}
			identity := packet(2, 0, mustPartyHex(t, capturedCharacterLevelInfo))
			deliver(identity)
			if r.characterLevel.known {
				t.Fatal("accepted identity before selection")
			}
			ack := mustPartyHex(t, "0100000000609c016b020000009c00000000")
			deliver(packet(4, 1, ack))
			notify := make([]byte, 41)
			notify[0] = 2
			deliver(packet(37, 0, notify))
			if r.characterLevel.known {
				t.Fatal("notification initialized missing identity")
			}
			deliver(identity)
			if r.characterLevel != (characterLevelInfo{known: true, uniqueID: 2, level: 1}) {
				t.Fatalf("missing initial level: %+v", r.characterLevel)
			}
			for _, state := range []ClientState{StateLogin, StateRun} {
				r.State = state
				for _, level := range []byte{2, 85, 86, 128, 255, 0, 1} {
					notify[0] = level
					deliver(packet(37, 0, notify))
					if r.characterLevel != (characterLevelInfo{known: true, uniqueID: 2, level: level}) {
						t.Fatalf("state=%v level=%d: %+v", state, level, r.characterLevel)
					}
				}
			}
			before := characterLevelInfo{known: true, uniqueID: 2, level: 85}
			r.characterLevel = before
			notify[0] = 99
			for _, state := range []ClientState{StateStop, StateSelect, StateWrong} {
				r.State = state
				deliver(packet(37, 0, notify))
				deliver(identity)
				if r.characterLevel != before {
					t.Fatalf("inactive state %d accepted identity update", state)
				}
			}
			r.State, r.ReturnSelectPending = StateRun, true
			notify[0] = 99
			deliver(packet(37, 0, notify))
			deliver(identity)
			if r.characterLevel != before {
				t.Fatal("pending return accepted level")
			}
			r.ReturnSelectPending = false
			r.characterUniqueID = 3
			deliver(packet(37, 0, notify))
			if r.characterLevel != before {
				t.Fatal("notification updated a stale identity")
			}
			r.State = StateLogin
			ack[9] = 4
			deliver(packet(4, 1, ack))
			if r.characterLevel.known || r.characterUniqueID != 4 {
				t.Fatal("new selection retained previous level")
			}
			for _, reset := range []func(){r.finishReturnToSelectUnsafe, func() { r.Load(UserLoginInfo{CID: 999}) }, r.CloseOut} {
				r.characterLevel = before
				reset()
				if r.characterLevel != (characterLevelInfo{}) {
					t.Fatal("session reset retained previous level")
				}
			}
		})
	}
}

func TestCharacterLevelRejectsUnrelatedAndDamagedPackets(t *testing.T) {
	r := NewRobotVo(nil)
	r.Cipher = newPartyTestCipher(t)
	r.State, r.characterUniqueID = StateRun, 2
	before := characterLevelInfo{known: true, uniqueID: 2, level: 85}
	r.characterLevel = before
	broken := mustPartyHex(t, capturedCharacterLevelInfo)
	broken[10] ^= 1
	for _, test := range []struct {
		typ  uint16
		flag byte
		body []byte
	}{
		{2, 1, mustPartyHex(t, capturedCharacterLevelInfo)}, {2, 0, broken},
		{2, 0, mustPartyHex(t, capturedCharacterBaseInfo)},
		{37, 1, make([]byte, 41)}, {137, 0, make([]byte, 41)},
		{37, 0, []byte{86}}, {37, 0, make([]byte, 40)}, {37, 0, make([]byte, 49)},
	} {
		raw := makePartyRecvPacket(test.typ, test.body)
		raw[0] = test.flag
		r.parsePacket(raw)
		if r.characterLevel != before {
			t.Fatalf("unrelated/damaged type=%d flag=%d replaced level", test.typ, test.flag)
		}
	}
}
