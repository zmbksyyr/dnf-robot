package dnf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"robot/internal/protocol/dnf/crypt"
)

func sceneCategoryLoginBody(name string, category byte) []byte {
	body := make([]byte, 11+len(name))
	body[0] = 1
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(name)))
	copy(body[5:], name)
	// Distinct preceding fields guard against reading the age/PC-room bytes.
	copy(body[5+len(name):], []byte{2, 3, 4, 127, 1, category})
	return body
}

func TestLoginSceneCategoryPrefix(t *testing.T) {
	for _, name := range []string{"", "1", "abcdefghijklmnopqrst"} {
		for value := 0; value <= 255; value++ {
			body := sceneCategoryLoginBody(name, byte(value))
			want := byte(value)
			if want == 5 {
				want = 4
			}
			got, ok := parseLoginSceneCategory(body)
			if !ok || !got.known || got.category != want {
				t.Fatalf("name=%q category=%d: %+v valid=%t", name, value, got, ok)
			}
			for size := 0; size < len(body); size++ {
				if _, ok = parseLoginSceneCategory(body[:size]); ok {
					t.Fatalf("truncated prefix accepted: name=%q bytes=%d", name, size)
				}
			}
			if _, ok = parseLoginSceneCategory(append(body, 0xf3, 0xa1)); !ok {
				t.Fatal("independent login tail was treated as category data")
			}
		}
	}
	for _, size := range []uint32{21, 22, 0xffffffff} {
		body := sceneCategoryLoginBody("abcdefghijklmnopqrstu", 14)
		binary.LittleEndian.PutUint32(body[1:5], size)
		if _, ok := parseLoginSceneCategory(body); ok {
			t.Fatalf("invalid native name length accepted: %d", size)
		}
	}
	body := sceneCategoryLoginBody("1", 14)
	body[0] = 0
	if _, ok := parseLoginSceneCategory(body); ok {
		t.Fatal("failed login supplied a scene category")
	}
}

func TestSceneCategoryLoginSessionCipherAndAnti(t *testing.T) {
	r := newLoginPacketTestRobot(t, &captureSessionConn{})
	body := sceneCategoryLoginBody("1", 14)
	body = append(body, make([]byte, alignTo(len(body), 16)-len(body))...)
	encrypted, err := r.Cipher.Encrypt(1, body)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 15+len(encrypted))
	raw[0] = 1
	binary.LittleEndian.PutUint16(raw[1:3], 1)
	binary.LittleEndian.PutUint32(raw[3:7], uint32(len(raw)))
	copy(raw[15:], encrypted)
	_, _, decoded, err := parseRecvPacket(r.Cipher, raw, false)
	if err != nil || !bytes.Equal(decoded, body) {
		t.Fatalf("flag-1 reply did not use session AES: body=%x err=%v", decoded, err)
	}
	r.parsePacket(raw)
	if !r.sceneCategory.known || r.sceneCategory.category != 14 {
		t.Fatalf("encrypted login category missing: %+v", r.sceneCategory)
	}
	copy(raw[15:], body)
	r.handleLoginPacketUnsafe(robotInboundPacket{typ: 1, flag: 1, data: raw, size: len(raw), isAnti: true})
	if !r.sceneCategory.known || r.sceneCategory.category != 14 {
		t.Fatalf("anti plaintext login category missing: %+v", r.sceneCategory)
	}
	copy(raw[15:], make([]byte, len(body)))
	r.handleLoginPacketUnsafe(robotInboundPacket{typ: 1, flag: 1, data: raw, size: len(raw), isAnti: true})
	if r.sceneCategory.known {
		t.Fatal("failed login retained stale category")
	}
	// The fixed key remains valid for the initial notification without session keys.
	raw[0] = 0
	cipher := crypt.NewDNFCipher()
	want, _ := cipher.DecryptLogin(raw[15:])
	_, _, got, err := parseRecvPacket(cipher, raw, false)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("initial handshake now requires a session key")
	}
}
