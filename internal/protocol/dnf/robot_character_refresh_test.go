package dnf

import (
	"encoding/binary"
	"testing"
	"time"

	"robot/internal/protocol/dnf/crypt"
)

func TestCharacterRefreshUsesCommandSevenAndReselectsSameCharacter(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateRun
	robot.CID = 900001
	robot.CharacterSlot = 7
	robot.PacketID = 41

	if !robot.ReturnToCharacterSelect() {
		t.Fatal("return-to-select request was not sent")
	}
	if len(conn.written) != 13 || conn.written[0] != 1 || binary.LittleEndian.Uint16(conn.written[1:3]) != 7 {
		t.Fatalf("return-to-select packet = %x", conn.written)
	}
	if robot.PacketID != 42 || !robot.ReturnSelectPending {
		t.Fatalf("return state packet_id=%d pending=%t", robot.PacketID, robot.ReturnSelectPending)
	}

	response := make([]byte, 16)
	response[0] = 0
	binary.LittleEndian.PutUint16(response[1:3], 7)
	binary.LittleEndian.PutUint32(response[3:7], uint32(len(response)))
	response[15] = 1
	robot.mu.Lock()
	robot.parsePacket(response)
	robot.mu.Unlock()
	if robot.State != StateSelect || robot.ReturnSelectPending || robot.ReturnSelectRejected {
		t.Fatalf("select ACK state=%d pending=%t rejected=%t", robot.State, robot.ReturnSelectPending, robot.ReturnSelectRejected)
	}

	conn.written = nil
	if !robot.ReselectCharacter() {
		t.Fatal("same character was not reselected")
	}
	if robot.State != StateLogin || !robot.SelectCharacSent {
		t.Fatalf("reselect state=%d sent=%t", robot.State, robot.SelectCharacSent)
	}
	if len(conn.written) < 13 || binary.LittleEndian.Uint16(conn.written[1:3]) != 4 {
		t.Fatalf("reselect packet = %x", conn.written)
	}
	payload, err := robot.Cipher.Decrypt(4, conn.written[13:])
	if err != nil {
		t.Fatal(err)
	}
	if payload[0] != robot.CharacterSlot {
		t.Fatalf("reselected slot = %d, want %d", payload[0], robot.CharacterSlot)
	}
}

func TestCharacterRefreshRejectKeepsCharacterOnlineState(t *testing.T) {
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.State = StateRun
	robot.ReturnSelectPending = true
	response := make([]byte, 16)
	binary.LittleEndian.PutUint16(response[1:3], 7)
	binary.LittleEndian.PutUint32(response[3:7], uint32(len(response)))
	response[15] = 0
	if !robot.handleReturnToSelectPacketUnsafe(robotInboundPacket{data: response, size: len(response), typ: 7}) {
		t.Fatal("pending command-7 response was not consumed")
	}
	if robot.State != StateRun || robot.ReturnSelectPending || !robot.ReturnSelectRejected {
		t.Fatalf("reject state=%d pending=%t rejected=%t", robot.State, robot.ReturnSelectPending, robot.ReturnSelectRejected)
	}
}

func TestCharacterRefreshAcceptsFlagOneAckWithoutBody(t *testing.T) {
	robot := NewRobotVo(nil)
	robot.State = StateRun
	robot.ReturnSelectPending = true
	response := make([]byte, 7)
	response[0] = 1
	binary.LittleEndian.PutUint16(response[1:3], 7)
	binary.LittleEndian.PutUint32(response[3:7], uint32(len(response)))
	if !robot.handleReturnToSelectPacketUnsafe(robotInboundPacket{data: response, size: len(response), flag: 1, typ: 7}) {
		t.Fatal("flag-one command-7 ACK was not consumed")
	}
	if robot.State != StateSelect || robot.ReturnSelectPending || robot.ReturnSelectRejected {
		t.Fatalf("flag-one ACK state=%d pending=%t rejected=%t", robot.State, robot.ReturnSelectPending, robot.ReturnSelectRejected)
	}
}

func TestPartyRecoveryAckClearsPartyAndReselectsCharacter(t *testing.T) {
	conn := &captureSessionConn{}
	robot := NewRobotVo(nil)
	robot.Cipher = crypt.NewDNFCipher()
	if err := robot.Cipher.Initialize(make([]byte, 334)); err != nil {
		t.Fatal(err)
	}
	robot.Conn = conn
	robot.State = StateRun
	robot.UID = 17000026
	robot.CharacterSlot = 3
	robot.partySelfPeer = partyIPPeer{uniqueID: 2, accID: robot.UID, slot: 1, slotKnown: true}
	robot.partyPeers[0] = partyIPPeer{uniqueID: 1, accID: 18000000, slot: 0, slotKnown: true}
	if !robot.sendReturnToCharacterSelectUnsafe(true) {
		t.Fatal("party recovery command was not sent")
	}

	response := make([]byte, 7)
	response[0] = 1
	binary.LittleEndian.PutUint16(response[1:3], 7)
	binary.LittleEndian.PutUint32(response[3:7], uint32(len(response)))
	if !robot.handleReturnToSelectPacketUnsafe(robotInboundPacket{data: response, size: len(response), flag: 1, typ: 7}) {
		t.Fatal("party recovery ACK was not consumed")
	}
	if robot.partyActiveUnsafe() {
		t.Fatal("party state survived the server ACK")
	}
	conn.written = nil

	deadline := time.Now().Add(2 * time.Second)
	for robot.State != StateLogin && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if robot.State != StateLogin || !robot.SelectCharacSent {
		t.Fatalf("character was not automatically reselected: state=%d sent=%t", robot.State, robot.SelectCharacSent)
	}
	if len(conn.written) < 3 || binary.LittleEndian.Uint16(conn.written[1:3]) != 4 {
		t.Fatalf("last recovery packet is not character select: %x", conn.written)
	}
}
