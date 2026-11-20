//go:build live

package dnf

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Live dungeon probe: logs a character in as a normal client and asks the
// original server to start a dungeon, then reports the session state. It is the
// protocol-level stand-in for a human leader, used only for validation and never
// built into the product binary (build tag "live").
//
// Environment:
//
//	DNF_LIVE_DSN     game user DSN, e.g. game:uu5!^%jg@tcp(127.0.0.1:3306)/d_taiwan
//	DNF_LIVE_KEY     path to the game privatekey.pem
//	DNF_LIVE_UID     account uid
//	DNF_LIVE_CID     character id
//	DNF_LIVE_SLOT    character slot index
//	DNF_LIVE_GAME    game address, default 127.0.0.1:10011
//	DNF_LIVE_DUNGEON dungeon index to enter (0 = skip the entry step)
//	DNF_LIVE_TYPE    dungeon difficulty byte (default 0)
//	DNF_LIVE_VILLAGE / DNF_LIVE_AREA / DNF_LIVE_X / DNF_LIVE_Y  birth position

// liveRequestBody is the padded request size used for the party and dungeon
// requests. The cipher needs a block multiple; the value is not yet confirmed
// against the original client.
const liveRequestBody = 16

func liveEnvInt(t *testing.T, name string, fallback int) int {	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q is not an integer", name, raw)
	}
	return value
}

func livePrivateKey(t *testing.T, path string) *rsa.PrivateKey {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		t.Fatal("private key is not RSA")
	}
	return rsaKey
}

func liveSnapshotLine(vo *RobotVo) string {
	s := vo.Snapshot()
	return fmt.Sprintf("state=%d err=%d reason=%d party=%t village=%d area=%d x=%d y=%d",
		s.State, s.LastError, s.DisconnectReason, s.PartyActive, s.Village, s.Area, s.X, s.Y)
}

func TestLiveDungeonProbe(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DNF_LIVE_DSN"))
	keyPath := strings.TrimSpace(os.Getenv("DNF_LIVE_KEY"))
	if dsn == "" || keyPath == "" {
		t.Skip("live probe needs DNF_LIVE_DSN and DNF_LIVE_KEY")
	}
	uid := liveEnvInt(t, "DNF_LIVE_UID", 17000015)
	cid := liveEnvInt(t, "DNF_LIVE_CID", 21)
	slot := liveEnvInt(t, "DNF_LIVE_SLOT", 1)
	gameAddr := strings.TrimSpace(os.Getenv("DNF_LIVE_GAME"))
	if gameAddr == "" {
		gameAddr = "127.0.0.1:10011"
	}
	host, portText, ok := strings.Cut(gameAddr, ":")
	if !ok {
		t.Fatalf("DNF_LIVE_GAME=%q must be host:port", gameAddr)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("DNF_LIVE_GAME port: %v", err)
	}
	dungeon := liveEnvInt(t, "DNF_LIVE_DUNGEON", 0)
	dungeonType := liveEnvInt(t, "DNF_LIVE_TYPE", 0)
	village := liveEnvInt(t, "DNF_LIVE_VILLAGE", 1)
	area := liveEnvInt(t, "DNF_LIVE_AREA", 0)
	birthX := liveEnvInt(t, "DNF_LIVE_X", 1000)
	birthY := liveEnvInt(t, "DNF_LIVE_Y", 300)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	key := livePrivateKey(t, keyPath)
	SetRSAKey(key)
	SetDBPool(db)

	info := UserLoginInfo{
		IP: host, Port: port, UID: uint32(uid), CID: cid, CharacterSlot: uint8(slot),
		MaxReConn: 3, ReDelay: 3,
		BirthPos: [4]uint32{uint32(village), uint32(area), uint32(birthX), uint32(birthY)},
	}
	setLoginToken(&info, resolveLoginToken("", uid, key))
	if info.TokenSize == 0 {
		t.Fatal("login token was not generated")
	}
	vo := NewRobotVo(db)
	vo.Load(info)
	loginIP := host
	if loginIP == "" || loginIP == "127.0.0.1" {
		loginIP = "127.0.0.1"
	}
	if !repairLoginPrerequisites(db, uid, loginIP) {
		t.Fatalf("login prerequisites were not repaired for uid=%d ip=%s", uid, loginIP)
	}
	task := NewRobotDnfTask()
	defer task.Shutdown()
	if !task.TryAddMessage("MsgOnLine", vo) {
		t.Fatal("online message was rejected by the task queue")
	}
	defer vo.CloseOut()

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := vo.Snapshot()
		if snapshot.State == StateRun || snapshot.State == StateWrong || snapshot.LastError != NoneError {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("after login: %s", liveSnapshotLine(vo))
	if vo.Snapshot().State != StateRun {
		t.Fatalf("character did not reach town: %s", liveSnapshotLine(vo))
	}

	if os.Getenv("DNF_LIVE_MOVE") == "1" {
		// Discrimination step: a SetPos packet uses the same send path as the
		// dungeon requests and is known to work from the robot runtime.
		moveBody := make([]byte, 8)
		binary.LittleEndian.PutUint16(moveBody[0:2], uint16(birthX+40))
		binary.LittleEndian.PutUint16(moveBody[2:4], uint16(birthY))
		moveBody[4] = 5
		binary.LittleEndian.PutUint16(moveBody[5:7], 100)
		movePacket, err := buildSendPacket(37, uint16(vo.PacketID), moveBody, vo.Cipher)
		if err != nil {
			t.Fatalf("build set position: %v", err)
		}
		vo.PacketID++
		if !vo.sendRaw(movePacket) {
			t.Fatal("set position was not sent")
		}
		t.Logf("sent set position")
		time.Sleep(2 * time.Second)
		t.Logf("after move: %s", liveSnapshotLine(vo))
	}

	// A real client answers the town loading notification with FinishLoad; the
	// server keeps the user out of the dungeon-capable state until it arrives.
	finish, err := buildSendPacket(40, uint16(vo.PacketID), buildFinishLoadingPayload(0, 0), vo.Cipher)
	if err != nil {
		t.Fatalf("build finish loading: %v", err)
	}
	vo.PacketID++
	if !vo.sendRaw(finish) {
		t.Fatal("finish loading was not sent")
	}
	t.Logf("sent finish loading")
	time.Sleep(700 * time.Millisecond)

	if os.Getenv("DNF_LIVE_INVITE") == "1" {
		// Town entity records carry the unique ids of the other players in the
		// room. Invite each candidate as a party peer; NPCs simply do not answer
		// and the robot accepts.
		time.Sleep(6 * time.Second)
		vo.mu.Lock()
		candidates := make([]uint16, 0, len(vo.townEntityPositions))
		for uniqueID := range vo.townEntityPositions {
			candidates = append(candidates, uniqueID)
		}
		vo.mu.Unlock()
		vo.mu.Lock()
		for uniqueID, entity := range vo.townEntityPositions {
			t.Logf("town entity %d at (%d,%d)", uniqueID, entity.x, entity.y)
		}
		vo.mu.Unlock()
		t.Logf("town entities: %v", candidates)
		for _, uniqueID := range candidates {
			body := make([]byte, liveRequestBody)
			binary.LittleEndian.PutUint16(body[0:2], uniqueID)
			body[2] = 0 // party request
			packet, err := buildSendPacket(10, uint16(vo.PacketID), body, vo.Cipher)
			if err != nil {
				t.Fatalf("build peer request: %v", err)
			}
			vo.PacketID++
			if !vo.sendRaw(packet) {
				t.Fatalf("peer request %d was not sent", uniqueID)
			}
			t.Logf("invited %d", uniqueID)
			time.Sleep(700 * time.Millisecond)
			vo.mu.Lock()
			self := vo.partySelfPeer.uniqueID
			peers := len(vo.partyPeers)
			vo.mu.Unlock()
			if self != 0 {
				t.Logf("party formed: self=%d peers=%d", self, peers)
				break
			}
		}
		t.Logf("after invite: %s", liveSnapshotLine(vo))
		time.Sleep(2 * time.Second)
	}

	if os.Getenv("DNF_LIVE_PARTY_ONLY") == "1" {
		partyBody := make([]byte, liveRequestBody)
		partyBody[0] = 1
		partyBody[1] = 4
		partyPacket, err := buildSendPacket(12, uint16(vo.PacketID), partyBody, vo.Cipher)
		if err != nil {
			t.Fatalf("build set party info: %v", err)
		}
		vo.PacketID++
		if !vo.sendRaw(partyPacket) {
			t.Fatal("set party info was not sent")
		}
		t.Logf("sent party create only; holding")
		time.Sleep(70 * time.Second)
		return
	}

	if dungeon > 0 {
		// The original server only lets a party leader pick a dungeon, so a solo
		// leader first creates a one-member party (type 12: title index, member
		// limit, dungeon index, difficulty).
		partyBody := make([]byte, liveRequestBody)
		partyBody[0] = 1 // no custom title
		partyBody[1] = 4 // member limit
		partyBody[2], partyBody[3] = 0, 0
		partyBody[4] = 0
		partyPacket, err := buildSendPacket(12, uint16(vo.PacketID), partyBody, vo.Cipher)
		if err != nil {
			t.Fatalf("build set party info: %v", err)
		}
		vo.PacketID++
		if !vo.sendRaw(partyPacket) {
			t.Fatal("set party info was not sent")
		}
		t.Logf("sent party create")
		time.Sleep(2 * time.Second)
		t.Logf("after party: %s", liveSnapshotLine(vo))

		// The reference puts the party into its dungeon state from StartGame
		// (CParty::game_start -> GameWorld::goto_dungeon -> state 5), and the
		// dungeon selection only validates in that state, so start first.
		start, err := buildSendPacket(15, uint16(vo.PacketID), nil, vo.Cipher)
		if err != nil {
			t.Fatalf("build start game: %v", err)
		}
		vo.PacketID++
		if !vo.sendRaw(start) {
			t.Fatal("start game was not sent")
		}
		t.Logf("sent start game")
		time.Sleep(1500 * time.Millisecond)

		// The original client pads small request bodies to the cipher block
		// size; the server reads only the fields it knows.
		body := make([]byte, liveRequestBody)
		body[0], body[1] = byte(dungeon), byte(dungeon>>8)
		body[2], body[3], body[4] = byte(dungeonType), byte(liveEnvInt(t, "DNF_LIVE_FIELD10", 0)), byte(liveEnvInt(t, "DNF_LIVE_FIELD11", 0))
		selectDungeon, err := buildSendPacket(16, uint16(vo.PacketID), body, vo.Cipher)
		if err != nil {
			t.Fatalf("build select dungeon: %v", err)
		}
		vo.PacketID++
		if !vo.sendRaw(selectDungeon) {
			t.Fatal("select dungeon was not sent")
		}
		t.Logf("sent select dungeon idx=%d type=%d", dungeon, dungeonType)
	}

	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		t.Logf("t+%02ds %s%s", i+1, liveSnapshotLine(vo), liveDungeonState(vo))
	}
}

// liveDungeonState reports the runtime state the dungeon loop keeps. The public
// snapshot does not expose it, and this probe is in-package on purpose.
func liveDungeonState(vo *RobotVo) string {
	vo.mu.Lock()
	defer vo.mu.Unlock()
	if vo.partyDungeonEnteredAt.IsZero() {
		return " dungeon=no"
	}
	return fmt.Sprintf(" dungeon=yes actors=%d target=%x legacy=%t",
		len(vo.partyDungeonWorld.actors), vo.partyDungeonWorld.target, vo.partyDungeonLegacySkills)
}
