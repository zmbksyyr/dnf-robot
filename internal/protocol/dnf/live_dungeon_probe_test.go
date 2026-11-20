//go:build live

package dnf

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math"
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

// liveLog writes a probe stage log straight to stderr: the test framework buffers
// t.Logf until the run ends, which makes the file's ordering useless when the
// session dies mid-run.
func liveLog(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	// A dedicated file avoids every stream-redirection quirk: the stage order is
	// what makes a dysfunct run diagnosable.
	if path := os.Getenv("DNF_LIVE_STAGE_FILE"); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}
	fmt.Fprintln(os.Stderr, line)
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
	if os.Getenv("DNF_LIVE_REPAIR") == "1" {
		// A previous session can leave the account marked online; the Robot does
		// this repair for itself, and the probe needs it when it logs in as an
		// account that was just logged out.
		if !repairLoginPrerequisites(db, uid, host) {
			t.Logf("login repair reported a failure for uid=%d", uid)
		}
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
	liveLog("after login: %s", liveSnapshotLine(vo))
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

	if areaProbe := liveEnvInt(t, "DNF_LIVE_AREA_PROBE", -1); areaProbe >= 0 {
		// Observable check: does SetArea actually move the character? A real move
		// changes the town entities the server broadcasts for the new area.
		townCount := func() int {
			vo.mu.Lock()
			defer vo.mu.Unlock()
			return len(vo.townEntityPositions)
		}
		time.Sleep(2 * time.Second)
		before := townCount()
		body := make([]byte, 12)
		body[0], body[1] = byte(village), byte(areaProbe)
		binary.LittleEndian.PutUint16(body[2:4], uint16(birthX))
		binary.LittleEndian.PutUint16(body[4:6], uint16(birthY))
		body[7] = 0x01
		if packet, err := buildSendPacket(38, uint16(vo.PacketID), body, vo.Cipher); err == nil {
			vo.PacketID++
			vo.sendRaw(packet)
			t.Logf("area probe: sent set area %d (entities before=%d)", areaProbe, before)
		}
		time.Sleep(3 * time.Second)
		t.Logf("area probe: after area %d entities=%d %s", areaProbe, townCount(), liveSnapshotLine(vo))
		return
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

	// Announce where we stand: the world-map index that normal dungeon selection
	// needs comes from the area the user reports, not from the character record.
	setArea := make([]byte, 12)
	setArea[0], setArea[1] = byte(village), byte(area)
	binary.LittleEndian.PutUint16(setArea[2:4], uint16(birthX))
	binary.LittleEndian.PutUint16(setArea[4:6], uint16(birthY))
	setArea[7] = 0x01
	if setAreaPacket, err := buildSendPacket(38, uint16(vo.PacketID), setArea, vo.Cipher); err == nil {
		vo.PacketID++
		if vo.sendRaw(setAreaPacket) {
			t.Logf("sent set area village=%d area=%d", village, area)
		}
	}
	if os.Getenv("DNF_LIVE_INVITE") == "1" {
		// Town entity records carry the unique ids of the other players in the
		// room. Invite each candidate as a party peer; NPCs simply do not answer
		// and the robot accepts.
		// Entities only appear as other players move, so keep scanning and invite
		// each newly seen peer until a party forms or the window expires.
		if peerID := liveEnvInt(t, "DNF_LIVE_PEER", 0); peerID > 0 {
			// The server log names the peer's session id directly; using it avoids
			// depending on whether the peer happened to walk into view.
			for attempt := 0; attempt < 40; attempt++ {
				body := make([]byte, liveRequestBody)
				binary.LittleEndian.PutUint16(body[0:2], uint16(peerID))
				body[2] = 0
				packet, err := buildSendPacket(10, uint16(vo.PacketID), body, vo.Cipher)
				if err != nil {
					t.Fatalf("build peer request: %v", err)
				}
				vo.PacketID++
				if !vo.sendRaw(packet) {
					t.Logf("direct peer request %d was not sent", peerID)
					break
				}
				time.Sleep(700 * time.Millisecond)
				vo.mu.Lock()
				self := vo.partySelfPeer.uniqueID
				vo.mu.Unlock()
				if self != 0 {
					t.Logf("party formed with %d: self=%d", peerID, self)
					break
				}
			}
			t.Logf("direct invite finished: %s", liveSnapshotLine(vo))
			time.Sleep(2 * time.Second)
		} else {		invited := map[uint16]bool{}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			vo.mu.Lock()
			pending := make([]uint16, 0, len(vo.townEntityPositions))
			for uniqueID := range vo.townEntityPositions {
				if !invited[uniqueID] {
					pending = append(pending, uniqueID)
				}
			}
			self := vo.partySelfPeer.uniqueID
			vo.mu.Unlock()
			if self != 0 {
				t.Logf("party formed: self=%d", self)
				break
			}
			for _, uniqueID := range pending {
				invited[uniqueID] = true
				vo.mu.Lock()
				entity := vo.townEntityPositions[uniqueID]
				vo.mu.Unlock()
				if int(entity.x) == birthX && int(entity.y) == birthY {
					t.Logf("skipping self %d", uniqueID)
					continue
				}
				body := make([]byte, liveRequestBody)
				binary.LittleEndian.PutUint16(body[0:2], uniqueID)
				body[2] = 0
				packet, err := buildSendPacket(10, uint16(vo.PacketID), body, vo.Cipher)
				if err != nil {
					t.Fatalf("build peer request: %v", err)
				}
				vo.PacketID++
				if !vo.sendRaw(packet) {
					t.Logf("peer request %d was not sent", uniqueID)
					continue
				}
				t.Logf("invited %d at (%d,%d)", uniqueID, entity.x, entity.y)
			}
			time.Sleep(600 * time.Millisecond)
		}
		vo.mu.Lock()
		peer := vo.partyPeers
		vo.mu.Unlock()
		t.Logf("party peers after invite: %d", len(peer))
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
		if os.Getenv("DNF_LIVE_INVITE") != "1" {
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
			t.Logf("sent party create")
			time.Sleep(2 * time.Second)
		}
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
		if liveEnvInt(t, "DNF_LIVE_SCAN_DUNGEON", 0) == 1 {
			// Which dungeon ids does the current world map accept? The reply body
			// tells us: 8 means "not selectable here", anything else is progress.
			for _, id := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
				probe := make([]byte, liveRequestBody)
				probe[0], probe[1] = byte(id), byte(id>>8)
				packet, err := buildSendPacket(16, uint16(vo.PacketID), probe, vo.Cipher)
				if err != nil {
					t.Fatalf("build select dungeon %d: %v", id, err)
				}
				vo.PacketID++
				if !vo.sendRaw(packet) {
					t.Logf("select dungeon %d was not sent", id)
					break
				}
				t.Logf("scan: asked for dungeon %d", id)
				time.Sleep(900 * time.Millisecond)
			}
			return
		}
		liveLog("sent select dungeon idx=%d type=%d", dungeon, dungeonType)

		// A real client acknowledges the dungeon room loading and then walks.
		// The party broadcast of that walking is what the Robot's follow loop
		// consumes, so the probe drives itself around after the room loads.
		// Only answer once the room actually starts. Sending FinishLoad for a room
		// the server never opened makes it drop the session instead of reporting why
		// the selection was refused.
		startedBy := time.Now().Add(8 * time.Second)
		roomStarted := false
		for time.Now().Before(startedBy) {
			vo.mu.Lock()
			roomStarted = !vo.partyDungeonEnteredAt.IsZero()
			vo.mu.Unlock()
			if roomStarted {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !roomStarted {
			liveLog("dungeon room did not start; selection was refused")
		}
		if roomStarted {
			finish, err := buildSendPacket(40, uint16(vo.PacketID), buildFinishLoadingPayload(0, 0), vo.Cipher)
			if err != nil {
				t.Fatalf("build dungeon finish loading: %v", err)
			}
			vo.PacketID++
			if vo.sendRaw(finish) {
				liveLog("sent dungeon finish loading")
			}
		}
		if one := os.Getenv("DNF_LIVE_MOVE_ONE"); one != "" {
			// One room move per session: an out-of-range coordinate makes the server
			// drop the connection, so the sweep has to be split across runs.
			var cx, cy int
			if _, scanErr := fmt.Sscanf(one, "%d,%d", &cx, &cy); scanErr == nil {
				time.Sleep(4 * time.Second)
				// MSG_MOVE_MAP starts with two *bytes* (x, y) followed by ints,
				// shorts and an array; a short-body request fails the reader and
				// the dispatcher answers by dropping the connection.
				moveBody := make([]byte, 48)
				moveBody[0], moveBody[1] = byte(cx), byte(cy)
				moveMap, buildErr := buildSendPacket(48, uint16(vo.PacketID), moveBody, vo.Cipher)
				if buildErr != nil {
					t.Fatalf("build single move map: %v", buildErr)
				}
				vo.PacketID++
				if vo.sendRaw(moveMap) {
					liveLog("sent single move map cell=(%d,%d)", cx, cy)
				} else {
					t.Logf("single move map was not sent")
				}
				time.Sleep(3 * time.Second)
			}
		}
		if liveEnvInt(t, "DNF_LIVE_NEXT_ROOM", 0) == 1 {
			// The party only starts once every member has finished loading; give
			// the peer a chance to report before asking for a room change.
			time.Sleep(5 * time.Second)
			// Sweep plausible grid cells in one session: a real client sends the
			// target room cell, and the server only reacts to one that exists.
			for _, cell := range [][2]byte{{1, 0}, {0, 1}, {2, 0}, {0, 2}, {1, 1}} {
				// 43 bytes of fields, padded to the cipher block size.
				body := make([]byte, 48)
				body[0], body[1] = cell[0], cell[1]
				moveMap, err := buildSendPacket(48, uint16(vo.PacketID), body, vo.Cipher)
				if err != nil {
					t.Fatalf("build move map: %v", err)
				}
				vo.PacketID++
				if vo.sendRaw(moveMap) {
					t.Logf("sent move map room=(%d,%d)", cell[0], cell[1])
				} else {
					t.Logf("move map was not sent")
					break
				}
				time.Sleep(1500 * time.Millisecond)
			}
		}
		if liveEnvInt(t, "DNF_LIVE_POST_MOVE", 0) == 1 {
			// A new room has no monsters yet and the party stands somewhere else:
			// report exactly that and see whether the Robot stops attacking and
			// follows instead.
			liveLog("post-move: empty room, leader at (%d,%d)", birthX+600, birthY+200)
			liveLeadFrames(t, vo, birthX+600, birthY+200)
		}
		drive := liveEnvInt(t, "DNF_LIVE_DRIVE", 0)
		x, y := birthX, birthY
		for step := 0; step < drive; step++ {
			x += 30
			moveBody := make([]byte, 8)
			binary.LittleEndian.PutUint16(moveBody[0:2], uint16(x))
			binary.LittleEndian.PutUint16(moveBody[2:4], uint16(y))
			moveBody[4] = 5
			binary.LittleEndian.PutUint16(moveBody[5:7], 100)
			movePacket, err := buildSendPacket(37, uint16(vo.PacketID), moveBody, vo.Cipher)
			if err != nil {
				t.Fatalf("build dungeon move: %v", err)
			}
			vo.PacketID++
			if !vo.sendRaw(movePacket) {
				t.Logf("dungeon move %d was not sent", step)
			}
			time.Sleep(500 * time.Millisecond)
		}
		if drive > 0 {
			t.Logf("drove %d steps to x=%d", drive, x)
		}
		if os.Getenv("DNF_LIVE_LEAD_FRAMES") == "1" {
			liveLeadFrames(t, vo, x, y)
		}
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

// liveLeadFrames sends the leader's motion on the party UDP transport, which is
// where a real client's actions reach the Robot's follow loop.
func liveLeadFrames(t *testing.T, vo *RobotVo, startX, startY int) {
	t.Helper()
	peer := vo.partyPeerForSlotUnsafe(1)
	if peer.uniqueID == 0 || !peer.slotKnown {
		t.Logf("no robot peer for party frames")
		return
	}
	leader := partyDungeonPlayerReference(vo.partySelfPeer.uniqueID)
	x, y := float32(startX), float32(startY)
	sent := 0
	for step := 0; step < 40; step++ {
		x += 20
		entry := partyDungeonMotion{entity: leader, controller: leader, modes: 0x415555, flags: [3]byte{2, 0, 0x10}}
		for i, value := range []float32{x, y} {
			entry.modes &^= 3 << uint((9+i)*2)
			entry.values[9+i] = math.Float32bits(value)
		}
		// A real client's frame describes every entity it sees, so the robot
		// learns its own position from it. Include the peer entity as well.
		robotEntry := partyDungeonMotion{entity: partyDungeonPlayerReference(peer.uniqueID), controller: partyDungeonPlayerReference(peer.uniqueID), modes: 0x415555, flags: [3]byte{2, 0, 0x10}}
		for i, value := range []float32{x - 50, y} {
			robotEntry.modes &^= 3 << uint((9+i)*2)
			robotEntry.values[9+i] = math.Float32bits(value)
		}
		// The leader's client also broadcasts the monsters it sees; that is how
		// the Robot learns what to approach and attack.
		monsterRef := [4]byte{0x11, 2, 1, 3}
		if override := liveEnvInt(t, "DNF_LIVE_MONSTER_REF", 0); override > 0 {
			// A new room reports different monsters; the Robot must treat them as
			// fresh targets rather than the ones it already killed.
			monsterRef = [4]byte{byte(override), 2, 1, 3}
		}
		killAfter := liveEnvInt(t, "DNF_LIVE_KILL_AFTER", 0)
		monster := partyDungeonMotion{entity: monsterRef, controller: monsterRef, modes: 0x415555, flags: [3]byte{2, 0, 0x10}}
		for i, value := range []float32{x + 120, y} {
			monster.modes &^= 3 << uint((9+i)*2)
			monster.values[9+i] = math.Float32bits(value)
		}
		route := vo.partyRouteForPeerUnsafe(peer.slot)
		dead := killAfter > 0 && step >= killAfter
		if dead && step == killAfter {
			// Report the kill exactly once; afterwards the monster is simply gone
			// from the room, the same way a cleared room looks to the Robot.
			body, ok := livePartyCommandBody(23, monsterRef[:])
			if ok {
				if seq, ok := vo.nextPartyTQOSSequenceUnsafe(peer.slot, route, false); ok {
					if packet, err := buildPartyUnreliablePacketChecked(seq, vo.partySelfPeer.slot, vo.partyDungeonFlags, body); err == nil {
						vo.sendPartyTransportUnsafe(vo.partyUDPConn, peer, route, packet)
						t.Logf("reported monster death after %d steps", step)
					}
				}
			}
		}
		motions := []partyDungeonMotion{entry, robotEntry}
		if !dead {
			motions = append(motions, monster)
		}
		body, ok := buildPartyDungeonMotion(motions)
		if !ok {
			t.Fatalf("build leader motion")
		}
		sequence, ok := vo.nextPartyTQOSSequenceUnsafe(peer.slot, route, false)
		if !ok {
			t.Logf("leader frame %d: no sequence", step)
			return
		}
		packet, err := buildPartyUnreliablePacketChecked(sequence, vo.partySelfPeer.slot, vo.partyDungeonFlags, body)
		if err != nil {
			t.Fatalf("build leader frame: %v", err)
		}
		if _, err := vo.sendPartyTransportUnsafe(vo.partyUDPConn, peer, route, packet); err != nil {
			t.Logf("leader frame %d: %v", step, err)
			return
		}
		if os.Getenv("DNF_LIVE_LEGACY") != "0" && step%2 == 0 {
			// A real leader also shows its own legacy skill state; the Robot only
			// switches to the legacy attack transport after seeing one.
			legacy := buildPartyLegacySkillStateBody(vo.partySelfPeer.uniqueID, 8, []byte{byte(step % 4)}, partySkillToken(vo.UID, time.Now()))
			if seq, ok := vo.nextPartyTQOSSequenceUnsafe(peer.slot, route, false); ok {
				if packet, err := buildPartyUnreliablePacketChecked(seq, vo.partySelfPeer.slot, vo.partyDungeonFlags, legacy); err == nil {
					vo.sendPartyTransportUnsafe(vo.partyUDPConn, peer, route, packet)
				}
			}
		}
		sent++
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("sent %d leader party frames to slot=%d uid=%d", sent, peer.slot, peer.uniqueID)
}
// livePartyCommandBody encodes one party dungeon command body (command 6/12/20/23)
// the same way the Robot decodes it: a command word, a checksummed payload and a
// per-byte XOR mask.
func livePartyCommandBody(command uint16, plain []byte) ([]byte, bool) {
	if len(plain) < 4 {
		return nil, false
	}
	checksum := partyPayloadChecksum(plain)
	body := make([]byte, 7+len(plain))
	body[0] = 2
	binary.LittleEndian.PutUint16(body[1:3], command)
	copy(body[3:7], checksum[:4])
	for i, value := range plain {
		body[7+i] = value ^ 0x7e
	}
	return body, true
}
