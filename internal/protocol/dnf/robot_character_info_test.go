package dnf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"
)

// Original Windows server, ordinary job0/level1 town login, 2026-11-23.
// Mode 1/count 1/world unique 2; the 82-byte base block carries MP 1400.
// This is a base-stat fixture, not a current/final MP or skill-cost fixture.
const capturedCharacterBaseInfo = "789c636464606200816020e6606760a8606560f006425d20ac8042224030502bc317461345060613c508e902f133020e095c0c290c4c3c0b3418188a0f68b23328a26be22b3060605057b0e360904697facfc9caa8c7b892712bd336a61d8c9bd9d731ae65c422c4c006d721c2c0f0f2514c33001da21a52"

func compressCharacterInfo(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCharacterBasicInfoCapturedBaseUnitsAndIdentity(t *testing.T) {
	body := mustPartyHex(t, capturedCharacterBaseInfo)
	info, ok := parseCharacterBasicInfo(body, 2)
	if !ok || !info.known || info.uniqueID != 2 || info.baseMP() != 140 || info.experience != 0 || info.stamina != 100 {
		t.Fatalf("captured base info = %+v, ok=%t", info, ok)
	}
	if binary.LittleEndian.Uint32(info.stats[4:8]) != 1400 {
		t.Fatal("base wire units were lost")
	}
	for _, uniqueID := range []uint16{0, 1, 3} {
		if _, ok := parseCharacterBasicInfo(body, uniqueID); ok {
			t.Fatalf("accepted attributes for unknown/different character %d", uniqueID)
		}
	}
	// A server base MP need not be a multiple of ten: the native setter uses
	// integer division, and zero is a valid base value.
	plain, err := inflatePartyInfo(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, wireMP := range []uint32{0, 1409, 25001} {
		binary.LittleEndian.PutUint32(plain[17:21], wireMP)
		parsed, ok := parseCharacterBasicInfo(compressCharacterInfo(t, plain), 2)
		if !ok || parsed.baseMP() != wireMP/10 {
			t.Fatalf("wire MP %d: base=%d ok=%t", wireMP, parsed.baseMP(), ok)
		}
	}
	for _, stamina := range []byte{0, 10, 99, 100} {
		plain[13+characterBaseStatsSize] = stamina
		parsed, ok := parseCharacterBasicInfo(compressCharacterInfo(t, plain), 2)
		if !ok || parsed.stamina != stamina {
			t.Fatalf("wire stamina %d: info=%+v ok=%t", stamina, parsed, ok)
		}
	}
}

func TestCharacterBaseMPRegenUsesSignedNativeUnits(t *testing.T) {
	info, ok := parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
	if !ok {
		t.Fatal("captured attributes did not parse")
	}
	for job := 0; job <= 10; job++ {
		if regen, known := info.baseMPRegen(job); !known || regen != 10 {
			t.Fatalf("captured job %d recovery = %d known=%t", job, regen, known)
		}
	}
	for _, job := range []int{-1, 11, 256} {
		if _, known := info.baseMPRegen(job); known {
			t.Fatalf("unsupported job %d has known recovery", job)
		}
	}
	for _, test := range []struct {
		wire int16
		want int32
	}{{-32768, -655}, {-59, -1}, {-50, -1}, {-49, 0}, {0, 0}, {49, 0}, {50, 1}, {999, 19}, {32767, 655}} {
		binary.LittleEndian.PutUint16(info.stats[0x40:0x42], uint16(test.wire))
		if got, known := info.baseMPRegen(0); !known || got != test.want {
			t.Fatalf("wire=%d recovery=%d known=%t want=%d", test.wire, got, known, test.want)
		}
	}
	if _, known := (characterBasicInfo{}).baseMPRegen(0); known {
		t.Fatal("missing self attributes became zero recovery")
	}
}

func TestCharacterBasicInfoRejectsOtherLayoutsAndDamagedBlocks(t *testing.T) {
	body := mustPartyHex(t, capturedCharacterBaseInfo)
	plain, err := inflatePartyInfo(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []byte{0, 2, 3, 4} {
		other := append([]byte(nil), plain...)
		other[0] = mode
		if _, ok := parseCharacterBasicInfo(compressCharacterInfo(t, other), 2); ok {
			t.Fatalf("accepted USERINFO mode %d", mode)
		}
	}
	for _, count := range []uint16{0, 2, 65535} {
		other := append([]byte(nil), plain...)
		binary.LittleEndian.PutUint16(other[1:3], count)
		if _, ok := parseCharacterBasicInfo(compressCharacterInfo(t, other), 2); ok {
			t.Fatalf("accepted unsupported record count %d", count)
		}
	}
	for _, size := range []uint32{0, 82, 84, 0xffffffff} {
		other := append([]byte(nil), plain...)
		binary.LittleEndian.PutUint32(other[9:13], size)
		if _, ok := parseCharacterBasicInfo(compressCharacterInfo(t, other), 2); ok {
			t.Fatalf("accepted stat length %d", size)
		}
	}
	for length := 0; length < 96; length++ {
		if _, ok := parseCharacterBasicInfo(compressCharacterInfo(t, plain[:length]), 2); ok {
			t.Fatalf("accepted truncated stat block at %d", length)
		}
	}
	body[len(body)-1] ^= 1
	if _, ok := parseCharacterBasicInfo(body, 2); ok {
		t.Fatal("accepted damaged zlib checksum")
	}
}

func TestCharacterInfoDispatchTransportAndSessionLifetime(t *testing.T) {
	for _, transport := range []string{"encrypted", "plain", "anti"} {
		t.Run(transport, func(t *testing.T) {
			r := NewRobotVo(nil)
			r.Cipher = newPartyTestCipher(t)
			r.State, r.SelectCharacSent = StateLogin, true
			packet := func(typ uint16, flag byte, body []byte) robotInboundPacket {
				raw := makePartyRecvPacket(typ, body)
				if transport == "encrypted" {
					padded := make([]byte, alignTo16(len(body)))
					copy(padded, body)
					raw = makeEncryptedPartyRecvPacket(t, r.Cipher, typ, padded)
				}
				raw[0] = flag
				return robotInboundPacket{typ: typ, flag: flag, size: len(raw), data: raw, isAnti: transport == "anti"}
			}
			ack := mustPartyHex(t, "0100000000609c016b020000009c00000000")
			infoPacket := packet(2, 0, mustPartyHex(t, capturedCharacterBaseInfo))
			r.handleCharacterInfoUnsafe(infoPacket)
			if r.characterBaseInfo.known {
				t.Fatal("initialized attributes before character selection identity")
			}
			ackPacket := packet(4, 1, ack)
			if transport == "anti" {
				r.handleCharacterInfoUnsafe(ackPacket)
				r.handleCharacterInfoUnsafe(infoPacket)
			} else {
				r.parsePacket(ackPacket.data)
				r.parsePacket(infoPacket.data)
			}
			if r.characterUniqueID != 2 || !r.characterBaseInfo.known || r.characterBaseInfo.baseMP() != 140 {
				t.Fatalf("attributes missing after %s select/info: %+v", transport, r.characterBaseInfo)
			}
			before := r.characterBaseInfo
			before.stamina = 10
			r.characterBaseInfo = before
			r.handleCharacterInfoUnsafe(packet(2, 1, mustPartyHex(t, capturedCharacterBaseInfo)))
			if r.characterBaseInfo != before {
				t.Fatal("command response replaced notification attributes")
			}
			// While returning to selection, late USERINFO must not overwrite
			// the current attributes/equipment or replace the selected identity.
			for _, state := range []ClientState{StateLogin, StateRun} {
				r.State, r.ReturnSelectPending = state, true
				r.handleCharacterInfoUnsafe(infoPacket)
				if r.characterBaseInfo != before {
					t.Fatalf("pending return accepted late USERINFO in state %d", state)
				}
				other := append([]byte(nil), ack...)
				binary.LittleEndian.PutUint16(other[9:11], 3)
				r.handleCharacterInfoUnsafe(packet(4, 1, other))
				if r.characterUniqueID != 2 || r.characterBaseInfo != before {
					t.Fatalf("pending return accepted late selection in state %d", state)
				}
			}
			r.State, r.ReturnSelectPending = StateRun, false
			r.handleCharacterInfoUnsafe(infoPacket)
			if r.characterBaseInfo.stamina != 100 {
				t.Fatal("running character stopped accepting USERINFO")
			}
			r.finishReturnToSelectUnsafe()
			if r.characterUniqueID != 0 || r.characterBaseInfo.known {
				t.Fatal("character return retained attributes")
			}
			r.characterUniqueID, r.characterBaseInfo = 2, before
			r.Load(UserLoginInfo{CID: 999})
			if r.characterUniqueID != 0 || r.characterBaseInfo.known {
				t.Fatal("new login retained previous character attributes")
			}
			r.characterUniqueID, r.characterBaseInfo = 2, before
			r.CloseOut()
			if r.characterUniqueID != 0 || r.characterBaseInfo.known {
				t.Fatal("logout retained attributes")
			}
		})
	}
}
