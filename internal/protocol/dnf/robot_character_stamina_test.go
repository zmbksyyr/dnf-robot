package dnf

import (
	"fmt"
	"testing"
)

func TestCharacterStaminaNotificationsTransportAndIdentity(t *testing.T) {
	for _, typ := range []uint16{4, 33} {
		for _, transport := range []string{"encrypted", "plain", "anti"} {
			t.Run(fmt.Sprintf("%d/%s", typ, transport), func(t *testing.T) {
				r := NewRobotVo(nil)
				r.Cipher = newPartyTestCipher(t)
				r.State, r.SelectCharacSent = StateRun, true
				r.characterUniqueID = 2
				info, ok := parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
				if !ok {
					t.Fatal("captured character did not parse")
				}
				r.characterBaseInfo = info
				packet := func(value byte) robotInboundPacket {
					body := []byte{value}
					raw := makePartyRecvPacket(typ, body)
					if transport == "encrypted" {
						blockSize := 16
						if typ == 33 {
							blockSize = 8
						}
						body = make([]byte, blockSize)
						copy(body, []byte{value, 0xb2, 0xce, 0x8d, 0x59})
						raw = makeEncryptedPartyRecvPacket(t, r.Cipher, typ, body)
					}
					return robotInboundPacket{typ: typ, flag: 0, data: raw, size: len(raw), isAnti: transport == "anti"}
				}
				for _, state := range []ClientState{StateLogin, StateRun} {
					r.State = state
					for _, value := range []byte{10, 19, 0, 100} {
						p := packet(value)
						if p.isAnti {
							r.handleCharacterInfoUnsafe(p)
						} else {
							r.parsePacket(p.data)
						}
						want := info
						want.stamina = value
						if r.characterBaseInfo != want || r.characterUniqueID != 2 {
							t.Fatalf("state %v stamina %d: info=%+v", state, value, r.characterBaseInfo)
						}
					}
				}
				for _, state := range []ClientState{StateStop, StateWrong, StateSelect} {
					r.State = state
					r.handleCharacterInfoUnsafe(packet(10))
					if r.characterBaseInfo != info {
						t.Fatalf("state %v accepted stamina", state)
					}
				}
				r.State = StateRun
				r.ReturnSelectPending = true
				r.handleCharacterInfoUnsafe(packet(10))
				if r.characterBaseInfo != info {
					t.Fatal("pending character return accepted stamina")
				}
				r.ReturnSelectPending = false
				r.characterUniqueID = 3
				r.handleCharacterInfoUnsafe(packet(10))
				if r.characterBaseInfo != info {
					t.Fatal("different character identity accepted stamina")
				}
				r.characterUniqueID = 2
				r.characterBaseInfo = characterBasicInfo{}
				r.handleCharacterInfoUnsafe(packet(10))
				if r.characterBaseInfo != (characterBasicInfo{}) {
					t.Fatal("notification initialized missing character attributes")
				}
			})
		}
	}
}

func TestCharacterStaminaRejectsMalformedAndUnrelatedPackets(t *testing.T) {
	r := NewRobotVo(nil)
	r.Cipher = newPartyTestCipher(t)
	r.State, r.characterUniqueID = StateRun, 2
	info, _ := parseCharacterBasicInfo(mustPartyHex(t, capturedCharacterBaseInfo), 2)
	r.characterBaseInfo = info
	for _, test := range []struct {
		typ  uint16
		flag byte
		body []byte
	}{
		{4, 0, nil}, {4, 0, []byte{101}}, {33, 0, []byte{255}},
		{4, 0, []byte{10, 0}}, {33, 0, make([]byte, 16)},
		{4, 0, []byte{10, 0, 0, 0, 0, 0, 0, 0}},
		{33, 0, []byte{10, 0xb2, 0xce, 0x8d, 0x59, 0, 1, 0}},
		{4, 1, []byte{10}}, {33, 1, []byte{10}}, {137, 0, []byte{10}},
		{33, 1, mustPartyHex(t, capturedCharacterBaseInfo)},
	} {
		raw := makePartyRecvPacket(test.typ, test.body)
		raw[0] = test.flag
		r.parsePacket(raw)
		if r.characterBaseInfo != info {
			t.Fatalf("accepted type=%d flag=%d body=%x", test.typ, test.flag, test.body)
		}
	}
	// A legitimate raw padded body must not be confused with its unsuccessful
	// decryption candidate, whose leading byte alone might also be a percentage.
	for _, typ := range []uint16{4, 33} {
		blockSize := 16
		if typ == 33 {
			blockSize = 8
		}
		body := make([]byte, blockSize)
		copy(body, []byte{10, 0xb2, 0xce, 0x8d, 0x59})
		r.parsePacket(makePartyRecvPacket(typ, body))
		if r.characterBaseInfo.stamina != 10 {
			t.Fatalf("type %d lost the valid raw padded body", typ)
		}
		r.characterBaseInfo = info
	}
}
