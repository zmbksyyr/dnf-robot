package dnf

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestSwordWaveMatchesOriginalClientCreation(t *testing.T) {
	// state-combat-native1, original skill20/level34 at 16:50:39.944:
	// peer21 at +119 ms, destroy at +448 ms. No script-data payload.
	want := mustPartyHex(t, "00001101a501ffff00000100214e00002200000000f0594800012b0500002e01000000000000000000000000000100")
	body, ref := buildPartySwordWave(421, 0x4859, 34,
		partyDungeonPosition{x: 1313.446, y: 302, known: 7}, true, 0x12345678)
	got, sender, ok := decodePartyPassiveObjectBody(body)
	if !ok || sender != 421 || ref != [4]byte{0, 0xf0, 0x59, 0x48} || !bytes.Equal(got, want) {
		t.Fatalf("create=%x sender=%d ref=%x", got, sender, ref)
	}
}

func TestSwordWaveCastQueuesLearnedLevelAndWaitsForStateAck(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.partySkillNextAt = now
	r.partySkillCandidates = []partySkillCandidate{{skillIndex: 20, state: 24, stateData: []byte{20}, level: 7,
		itemCostFree: true, cooldown: 3 * time.Second}}
	sender, receiver := comboTestTransport(t, r)
	r.flushPartyDungeonSkillUnsafe(sender, now)
	readComboState(t, receiver, 24, 0, 2)
	if len(r.partyBasicProjectiles) != 1 || r.partySkillReadyAt[20] != now.Add(3*time.Second) {
		t.Fatal("wave cast did not queue its object and cooldown")
	}
	r.flushPartyBasicProjectilesUnsafe(sender, now.Add(200*time.Millisecond))
	if r.partyBasicProjectiles[0].created {
		t.Fatal("wave object overtook the unacknowledged state")
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
	r.flushPartyBasicProjectilesUnsafe(sender, now.Add(118*time.Millisecond))
	if r.partyBasicProjectiles[0].created {
		t.Fatal("wave object preceded its creation frame")
	}
	created := now.Add(119 * time.Millisecond)
	r.flushPartyBasicProjectilesUnsafe(sender, created)
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	bodies, ok := partyDungeonFrameBodies(buf[:n])
	if !ok || len(bodies) != 1 {
		t.Fatalf("missing object frame: %x", buf[:n])
	}
	data, _, ok := decodePartyPassiveObjectBody(bodies[0])
	if !ok || binary.LittleEndian.Uint32(data[12:16]) != 20001 || binary.LittleEndian.Uint32(data[16:20]) != 7 || data[25] != 0 {
		t.Fatalf("wave lost learned level or left-facing direction: %x", data)
	}
	object := r.partyBasicProjectiles[0]
	if !object.created || object.expires != created.Add(350*time.Millisecond) {
		t.Fatal("wave did not acquire its observed lifetime")
	}
	// A piercing wave retains its lifetime after its first collision.
	hit := buildPartyObjectDestroy(object.reference)
	hit[1] = 19
	r.observePartyProjectileHitUnsafe(hit, created.Add(time.Millisecond))
	if r.partyBasicProjectiles[0].expires != object.expires {
		t.Fatal("first hit destroyed the piercing wave")
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 1), 1, r.partyPeers[0])
	r.flushPartyBasicProjectilesUnsafe(sender, object.expires)
	if len(r.partyBasicProjectiles) != 0 {
		t.Fatal("expired wave was not destroyed")
	}
	// Leaving the room must also cancel an object not yet created.
	r.queuePartySkillObjectUnsafe(r.partySkillCandidates[0], now)
	r.clearPartyDungeonRuntimeUnsafe()
	if len(r.partyBasicProjectiles) != 0 {
		t.Fatal("room exit retained a scheduled wave")
	}
}
