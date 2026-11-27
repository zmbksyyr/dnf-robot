package dnf

import (
	"encoding/binary"
	"testing"

	"robot/internal/protocol/dnf/crypt"
)

func dungeonIdentityTestBody(id uint16, objects byte) []byte {
	body := make([]byte, 13+2*int(objects))
	binary.LittleEndian.PutUint16(body, id)
	body[9] = objects
	body[len(body)-1] = 11
	return body
}

func TestDungeonIdentityNotificationLayout(t *testing.T) {
	for _, count := range []byte{0, 1, 3, 255} {
		body := dungeonIdentityTestBody(65535, count)
		padded := append(append([]byte(nil), body...), 1, 2, 3, 4)
		padded = append(padded, make([]byte, alignTo(len(padded), 4)-len(padded))...)
		for _, data := range [][]byte{body, padded} {
			if got, valid := parseDungeonDefinitionIdentity(data); !valid || !got.known || got.id != 65535 {
				t.Fatalf("count%d length%d: %+v/%t", count, len(data), got, valid)
			}
		}
		for _, data := range [][]byte{body[:len(body)-1], append(append([]byte(nil), body...), 0), padded[:len(padded)-1], append(padded, 0)} {
			if _, valid := parseDungeonDefinitionIdentity(data); valid {
				t.Fatalf("accepted truncated/oversized count%d length%d", count, len(data))
			}
		}
		padded[len(padded)-1] = 1
		if _, valid := parseDungeonDefinitionIdentity(padded); !valid {
			t.Fatal("alignment bytes changed a valid notification identity")
		}
	}
}

func TestDungeonIdentityRejectsConflictingReceiveCandidates(t *testing.T) {
	r := NewRobotVo(nil)
	r.Cipher = crypt.NewDNFCipher()
	key := make([]byte, r.Cipher.KeySize())
	key[4] = 1
	if err := r.Cipher.Initialize(key); err != nil {
		t.Fatal(err)
	}
	body := dungeonIdentityTestBody(4, 0)
	body = append(body, make([]byte, 20-len(body))...)
	encrypted, err := r.Cipher.Encrypt(28, body)
	if err != nil {
		t.Fatal(err)
	}
	raw := append(make([]byte, 15), encrypted...)
	binary.LittleEndian.PutUint16(raw[1:3], 28)
	packet := robotInboundPacket{typ: 28, size: len(raw), data: raw}
	if identity := r.selectDungeonDefinitionIdentityUnsafe(packet); identity.known {
		t.Fatal("conflicting plain and decrypted identities selected", identity)
	}
	packet.isAnti = true
	packet.data = append(make([]byte, 15), dungeonIdentityTestBody(4, 0)...)
	if identity := r.selectDungeonDefinitionIdentityUnsafe(packet); !identity.known || identity.id != 4 {
		t.Fatal("anti identity unavailable", identity)
	}
}
