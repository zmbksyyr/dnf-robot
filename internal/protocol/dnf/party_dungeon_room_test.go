package dnf

import (
	"net"
	"testing"
	"time"
)

func TestNewDungeonRoomDiscardsOldActionsAndPreservesSkillCooldowns(t *testing.T) {
	now := time.Now()
	r, _, enemy := basicAttackTestRobot(now)
	r.partySkillJob = 8
	r.partySkillCandidates = []partySkillCandidate{{skillIndex: 3, state: 22}}
	r.partySkillReadyAt = map[byte]time.Time{3: now.Add(15 * time.Second)}
	sender, receiver := comboTestTransport(t, r)
	r.partyUDPConn = sender
	r.partyUDPRunning = true
	r.partySelfPeer.accID = r.UID
	r.partyPeers[0].accID = 43
	for _, purpose := range []string{"follow", "skill-cast"} {
		body := buildPartyLegacySkillStateBody(r.partySelfPeer.uniqueID, 8, []byte{2}, 1)
		if _, err := r.sendPartyReliableUnsafe(sender, r.partyPeers[0], 5, [][]byte{body}, purpose, now); err != nil {
			t.Fatal(err)
		}
		readComboState(t, receiver, 8, 2, 1)
	}
	r.partyBasicAttackStep = 3
	r.partyBasicAttackTarget = enemy
	r.partyBasicAttackNextAt = now.Add(partyBasicComboInterval)
	r.partySkillRecoverAt = now.Add(partySkillRecoverDelay)
	r.partyDungeonFollow = []partyDungeonFollowPending{{due: now.Add(time.Second), records: [][]byte{{1, 2, 3}}}}
	entered := now.Add(time.Second)
	r.markPartyDungeonEnteredUnsafe(entered)
	if len(r.partyDungeonFollow) != 0 || r.partyReliablePurposePendingUnsafe(0, "follow") || r.partyReliablePurposePendingUnsafe(0, "skill-") ||
		r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} || !r.partyBasicAttackNextAt.IsZero() || !r.partySkillRecoverAt.IsZero() {
		t.Fatal("previous room's movement, pending attacks or combo survived loading")
	}
	if len(r.partyDungeonWorld.actors) != 0 || r.partyDungeonEnteredAt != entered || r.partyDungeonLastAt != entered {
		t.Fatal("new room did not start with fresh actors and activity")
	}
	if !r.partySkillLoaded || len(r.partySkillCandidates) != 1 || r.partySkillReadyAt[3] != now.Add(15*time.Second) {
		t.Fatal("room loading lost the learned profile or reset a real skill cooldown")
	}
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	packet, ok := parsePartyTQOSPacket(buf[:n], 1)
	if !ok || packet.state != 3 {
		t.Fatalf("cancelling old reliable actions did not reopen their epoch: %x", buf[:n])
	}
	r.flushPartyReliableUnsafe(sender, entered.Add(2*time.Second))
	receiver.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, _, err := receiver.ReadFromUDP(buf); err == nil {
		t.Fatal("old room action retransmitted after new room loading")
	} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatal(err)
	}
}

func TestNewDungeonRoomKeepsTransportEpochWhenNoOldActionsArePending(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(now)
	r.partyTQOSReliableSeq[0][1] = 123
	r.partyTQOSSeq[0][1] = 456
	r.partyDungeonLastAt = now.Add(-time.Minute)
	r.markPartyDungeonEnteredUnsafe(now.Add(time.Second))
	if r.partyTQOSReliableSeq[0][1] != 123 || r.partyTQOSSeq[0][1] != 456 || r.partyDungeonLastAt != now.Add(time.Second) {
		t.Fatal("loading reset an unrelated transport epoch or retained old room activity")
	}
}
