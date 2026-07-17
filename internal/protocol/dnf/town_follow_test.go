package dnf

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"robot/internal/protocol/dnf/crypt"
	"testing"
	"time"
)

func TestParseTownEntityPackets(t *testing.T) {
	position, ok := parseTownEntityPosition(mustPartyHex(t, "92968e03160106c8006459f557000000"))
	if !ok || position.uniqueID != 0x9692 || position.x != 910 || position.y != 278 || position.moveType != 6 || position.speed != 200 || position.seenAt.IsZero() {
		t.Fatalf("position = %+v ok=%v", position, ok)
	}
	area, ok := parseTownEntityArea(mustPartyHex(t, "929603018e03160100000000000000000000"))
	if !ok || area.uniqueID != 0x9692 || area.village != 3 || area.area != 1 || area.x != 910 || area.y != 278 {
		t.Fatalf("area = %+v ok=%v", area, ok)
	}
}

func TestRobotFollowsCachedPartyLeaderTownPosition(t *testing.T) {
	cipher := crypt.NewDNFCipher()
	if err := cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robotConn, peerConn := net.Pipe()
	defer robotConn.Close()
	defer peerConn.Close()

	vo := &RobotVo{State: StateRun, Cipher: cipher, Conn: robotConn, PacketID: 7, CurX: 100, CurY: 200}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x24ee, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x9692, slot: 0, slotKnown: true}
	if _, ok := vo.rememberTownEntityUnsafe(mustPartyHex(t, "92968e03160106c8006459f557000000")); !ok {
		t.Fatal("town position was not cached")
	}

	gotCh := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 21)
		_, _ = io.ReadFull(peerConn, buf)
		gotCh <- buf
	}()
	if !vo.followCachedPartyLeaderTownPositionUnsafe() {
		t.Fatal("cached leader position did not trigger follow")
	}
	got := <-gotCh
	if got[0] != 1 || binary.LittleEndian.Uint16(got[1:3]) != 37 || binary.LittleEndian.Uint16(got[11:13]) != 7 {
		t.Fatalf("packet header = %x", got[:13])
	}
	payload, err := cipher.Decrypt(37, got[13:])
	if err != nil {
		t.Fatal(err)
	}
	if want := mustPartyHex(t, "8e03160106c80000"); !bytes.Equal(payload, want) {
		t.Fatalf("payload = %x, want %x", payload, want)
	}
	if vo.PacketID != 8 || vo.CurX != 910 || vo.CurY != 278 || vo.MoveType != 6 {
		t.Fatalf("state packet=%d x=%d y=%d type=%d", vo.PacketID, vo.CurX, vo.CurY, vo.MoveType)
	}
}

func TestRobotFollowsPartyLeaderTownArea(t *testing.T) {
	cipher := crypt.NewDNFCipher()
	if err := cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robotConn, peerConn := net.Pipe()
	defer robotConn.Close()
	defer peerConn.Close()

	vo := &RobotVo{State: StateRun, Cipher: cipher, Conn: robotConn, PacketID: 11, CurVillage: 3, CurArea: 0, CurX: 100, CurY: 200}
	vo.partySelfPeer = partyIPPeer{uniqueID: 0x24ee, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x9692, slot: 0, slotKnown: true}

	gotCh := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 29)
		_, _ = io.ReadFull(peerConn, buf)
		gotCh <- buf
	}()
	if !vo.followPartyLeaderTownAreaUnsafe(townEntityArea{uniqueID: 0x9692, village: 3, area: 1, x: 910, y: 278}) {
		t.Fatal("leader area did not trigger follow")
	}
	got := <-gotCh
	if got[0] != 1 || binary.LittleEndian.Uint16(got[1:3]) != 38 || binary.LittleEndian.Uint16(got[11:13]) != 11 {
		t.Fatalf("packet header = %x", got[:13])
	}
	payload, err := cipher.Decrypt(38, got[13:])
	if err != nil {
		t.Fatal(err)
	}
	want := mustPartyHex(t, "03018e03160100010300000000000000")
	if !bytes.Equal(payload, want) {
		t.Fatalf("payload = %x, want %x", payload, want)
	}
}

func TestTownFollowRequiresSlotZeroLeader(t *testing.T) {
	vo := &RobotVo{State: StateRun, CurVillage: 3, CurArea: 0, CurX: 100, CurY: 200}
	vo.partySelfPeer = partyIPPeer{uniqueID: 1, slot: 0, slotKnown: true}
	position := townEntityPosition{uniqueID: 2, x: 910, y: 278, moveType: 6, speed: 200, seenAt: time.Now()}
	area := townEntityArea{uniqueID: 2, village: 3, area: 1, x: 910, y: 278}
	if vo.followPartyLeaderTownPositionUnsafe(position) || vo.followPartyLeaderTownAreaUnsafe(area) {
		t.Fatal("party leader followed another member")
	}
	vo.partySelfPeer = partyIPPeer{uniqueID: 1, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 3, slot: 0, slotKnown: true}
	if vo.followPartyLeaderTownPositionUnsafe(position) || vo.followPartyLeaderTownAreaUnsafe(area) {
		t.Fatal("party member followed a non-leader")
	}
}

func TestRobotSuppressesOrdinaryActionsWhilePartyActive(t *testing.T) {
	vo := &RobotVo{State: StateRun, PacketID: 9, CurVillage: 3, CurArea: 0, CurX: 100, CurY: 200}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 2}

	vo.SetPosition(910, 278, 6, 200)
	vo.SetArea(3, 1, 910, 278)
	vo.SendPublicMessage(0, []byte("test"))
	vo.CreatePrivateStore()
	if vo.OpenDisjointStore(500) {
		t.Fatal("disjoint store started while party active")
	}
	if vo.PacketID != 9 || vo.CurArea != 0 || vo.CurX != 100 || vo.CurY != 200 || vo.RobotTyp != 0 {
		t.Fatalf("ordinary action changed grouped robot: packet=%d type=%d area=%d x=%d y=%d", vo.PacketID, vo.RobotTyp, vo.CurArea, vo.CurX, vo.CurY)
	}
	if !vo.Snapshot().PartyActive {
		t.Fatal("snapshot did not expose party state")
	}
}

func TestRobotIgnoresStaleCachedLeaderPosition(t *testing.T) {
	vo := &RobotVo{State: StateRun, CurX: 100, CurY: 200}
	vo.partySelfPeer = partyIPPeer{uniqueID: 1, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 2, slot: 0, slotKnown: true}
	vo.townEntityPositions = map[uint16]townEntityPosition{
		2: {uniqueID: 2, x: 910, y: 278, moveType: 6, speed: 200, seenAt: time.Now().Add(-partyTownPositionMaxAge - time.Second)},
	}
	if vo.followCachedPartyLeaderTownPositionUnsafe() {
		t.Fatal("stale cached leader position triggered follow")
	}
}

func TestPartyLeaveNotificationClearsState(t *testing.T) {
	cipher := crypt.NewDNFCipher()
	if err := cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint16(body[:2], 0x9692)
	encrypted, err := cipher.Encrypt(6, body)
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 15+len(encrypted))
	binary.LittleEndian.PutUint16(packet[1:3], 6)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	copy(packet[15:], encrypted)

	vo := &RobotVo{State: StateRun, Cipher: cipher}
	vo.partySelfPeer = partyIPPeer{uniqueID: 1, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{uniqueID: 0x9692, slot: 0, slotKnown: true}
	vo.parsePacket(packet)
	if vo.partyActiveUnsafe() || vo.partySelfPeer.uniqueID != 0 {
		t.Fatalf("party state remained after leave: self=%+v peers=%+v", vo.partySelfPeer, vo.partyPeers)
	}
}
