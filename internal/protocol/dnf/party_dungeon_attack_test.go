package dnf

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"robot/internal/shared"
)

func skillTargetTestWorld(selfID uint16, now time.Time) partyDungeonWorld {
	return partyDungeonWorld{actors: map[[4]byte]partyDungeonActor{
		partyDungeonPlayerReference(selfID): {position: partyDungeonPosition{x: 100, y: 300, known: 7}, seenAt: now},
		{0x11, 2, 1, 3}:                     {position: partyDungeonPosition{x: 30, y: 300, known: 7}, seenAt: now},
	}}
}

func TestActiveSkillTargetsLiveEnemyBeforeSendingAndRecoversAfterItsDeath(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "envelope", true: "legacy"}[legacy], func(t *testing.T) {
			now := time.Now()
			r, self, enemy := basicAttackTestRobot(t, now)
			r.partyDungeonLegacySkills = legacy
			r.partySkillJob = 8
			r.partySkillNextAt = now
			r.partySkillCandidates = []partySkillCandidate{{itemCostFree: true, skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}, cooldown: 15 * time.Second}}
			receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer receiver.Close()
			sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			remote := receiver.LocalAddr().(*net.UDPAddr)
			r.partyPeers[0].outerIP, r.partyPeers[0].port = remote.IP, uint16(remote.Port)
			before := r.partyDungeonWorld.actors[self].position
			r.flushPartyDungeonSkillUnsafe(sender, now)
			if r.partyDungeonWorld.targetAttackAt != now || r.partyDungeonWorld.actors[enemy].lastAttackAt != now {
				t.Fatal("sent active skill was not tracked for combat progress")
			}
			buf := make([]byte, 4096)
			receiver.SetReadDeadline(time.Now().Add(time.Second))
			n, _, err := receiver.ReadFromUDP(buf)
			if err != nil {
				t.Fatal(err)
			}
			bodies, ok := partyDungeonFrameBodies(buf[:n])
			if !ok || len(bodies) != 2 {
				t.Fatalf("skill was not preceded by facing: %x", buf[:n])
			}
			motion, ok := decodePartyDungeonMotion(bodies[0])
			if !ok || len(motion) != 1 || motion[0].entity != self || motion[0].controller != self || motion[0].flags[2]&0x20 != 0 || before.apply(motion[0]) != before {
				t.Fatalf("skill did not face the enemy without moving: %+v", motion)
			}
			if motion[0].flags[0] != 1 || motion[0].flags[1] != 2 {
				t.Fatalf("skill facing changed the room: %+v", motion[0])
			}
			wantCommand := uint16(partyDungeonEnvelopeCommand)
			if legacy {
				wantCommand = 5
			}
			if binary.LittleEndian.Uint16(bodies[1][1:3]) != wantCommand || r.partySkillReadyAt[3] != now.Add(15*time.Second) {
				t.Fatal("skill wire or cooldown changed")
			}
			actor := r.partyDungeonWorld.actors[enemy]
			actor.dead = true
			r.partyDungeonWorld.actors[enemy] = actor
			r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
			r.flushPartyDungeonSkillUnsafe(sender, now.Add(partySkillRecoverDelay))
			n, _, err = receiver.ReadFromUDP(buf)
			if err != nil {
				t.Fatal(err)
			}
			bodies, ok = partyDungeonFrameBodies(buf[:n])
			if !ok || len(bodies) != 1 || !r.partySkillRecoverAt.IsZero() {
				t.Fatal("last enemy dying prevented recovery")
			}
		})
	}
}

func TestActiveSkillSkipsEmptyDeadAndUnknownTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RobotVo, [4]byte, [4]byte, time.Time)
	}{
		{"empty", func(r *RobotVo, self, enemy [4]byte, now time.Time) { delete(r.partyDungeonWorld.actors, enemy) }},
		{"dead", func(r *RobotVo, self, enemy [4]byte, now time.Time) {
			a := r.partyDungeonWorld.actors[enemy]
			a.dead = true
			r.partyDungeonWorld.actors[enemy] = a
		}},
		{"unknown-target", func(r *RobotVo, self, enemy [4]byte, now time.Time) {
			a := r.partyDungeonWorld.actors[enemy]
			a.position.known = 1
			r.partyDungeonWorld.actors[enemy] = a
		}},
		{"unknown-self", func(r *RobotVo, self, enemy [4]byte, now time.Time) { delete(r.partyDungeonWorld.actors, self) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			r, self, enemy := basicAttackTestRobot(t, now)
			r.partySkillJob = 8
			r.partySkillNextAt = now
			r.partySkillCandidates = []partySkillCandidate{{itemCostFree: true, skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}, cooldown: 15 * time.Second}}
			tc.change(r, self, enemy, now)
			receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer receiver.Close()
			sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			remote := receiver.LocalAddr().(*net.UDPAddr)
			r.partyPeers[0].outerIP, r.partyPeers[0].port = remote.IP, uint16(remote.Port)
			r.flushPartyDungeonSkillUnsafe(sender, now)
			if r.partyTQOSReliableSeq[0][1] != 0 || !r.partySkillRecoverAt.IsZero() || len(r.partySkillReadyAt) != 0 {
				t.Fatal("cast without a current target consumed transport, recovery or cooldown")
			}
		})
	}
}

func basicAttackTestRobot(t *testing.T, now time.Time) (*RobotVo, [4]byte, [4]byte) {
	setPartyCombatTestEnabled(t)
	world, self, _, enemy := navigationTestWorld(now)
	actor := world.actors[enemy]
	actor.position.x = 30 // The target crossed behind our stationary actor.
	world.actors[enemy] = actor
	r := &RobotVo{UID: 42, partyDungeonWorld: world,
		partyDungeonEnteredAt: now, partyDungeonLastAt: now,
		partySelfPeer:    partyIPPeer{uniqueID: 5, slot: 1, slotKnown: true},
		partySkillLoaded: true, partySkillJob: 0, partyDungeonLegacySkills: true,
		partySkillNextAt:            now.Add(5 * time.Second),
		partySkillCatalogGeneration: shared.PartySkillStateGeneration(),
	}
	r.partyPeers[0] = partyIPPeer{uniqueID: 4, slot: 0, slotKnown: true}
	return r, self, enemy
}

func TestNormalAttackFacesOwnTargetAndWaitsForAcknowledgementBeforeRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		job  int
	}{{"swordman", 0}, {"gunner", 2}, {"male-mage", 8}} {
		t.Run(tc.name, func(t *testing.T) {
			testNormalAttackFacesOwnTargetAndWaitsForAcknowledgementBeforeRecovery(t, tc.job)
		})
	}
}

func testNormalAttackFacesOwnTargetAndWaitsForAcknowledgementBeforeRecovery(t *testing.T, job int) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	r.partySkillJob = job
	r.partySkillNextAt = now // An empty active-skill list must still permit normal attacks.
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	remote := receiver.LocalAddr().(*net.UDPAddr)
	r.partyPeers[0].outerIP, r.partyPeers[0].port = remote.IP, uint16(remote.Port)
	r.partyDungeonFollow = []partyDungeonFollowPending{{due: now.Add(time.Second)}}
	before := r.partyDungeonWorld.actors[self].position
	r.flushPartyDungeonSkillUnsafe(sender, now)
	if r.partyDungeonWorld.targetAttackAt != now || r.partyDungeonWorld.actors[r.partyDungeonWorld.target].lastAttackAt != now {
		t.Fatal("sent normal attack was not tracked for combat progress")
	}
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	bodies, ok := partyDungeonFrameBodies(buf[:n])
	if !ok || len(bodies) != 2 {
		t.Fatalf("ordered facing/attack missing: %x", buf[:n])
	}
	motion, ok := decodePartyDungeonMotion(bodies[0])
	if !ok || len(motion) != 1 || motion[0].entity != self || motion[0].controller != self || motion[0].flags[2]&0x20 != 0 || before.apply(motion[0]) != before {
		t.Fatalf("attack changed another actor, moved, or faced away: %+v", motion)
	}
	if motion[0].flags[0] != 1 || motion[0].flags[1] != 2 {
		t.Fatalf("normal attack facing changed the room: %+v", motion[0])
	}
	if !validPartyLegacySkillBody(bodies[1], 5) {
		t.Fatalf("invalid state body: %x", bodies[1])
	}
	p := append([]byte(nil), bodies[1][7:]...)
	for i := range p {
		p[i] ^= 0x7e
	}
	if len(p) != 10 || p[4] != 8 || p[7] != 0 {
		t.Fatalf("normal attack vector changed: %x", p)
	}
	interval := partyBasicAttackInterval
	if job == 8 {
		interval = partyBasicComboInterval
	}
	if r.partyBasicAttackNextAt != now.Add(interval) || r.partySkillRecoverAt != now.Add(partySkillRecoverDelay) || len(r.partySkillReadyAt) != 0 || len(r.partyDungeonFollow) != 0 {
		t.Fatal("normal attack did not set its recovery/cadence or altered skill cooldowns")
	}
	r.flushPartyDungeonSkillUnsafe(sender, now.Add(partySkillRecoverDelay))
	if r.partySkillRecoverAt != now.Add(partySkillRecoverDelay+partySkillRecoveryRetry) {
		t.Fatal("recovery ignored the pending attack ACK")
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
	r.flushPartyDungeonSkillUnsafe(sender, now.Add(partySkillRecoverDelay+partySkillRecoveryRetry))
	n, _, err = receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	bodies, ok = partyDungeonFrameBodies(buf[:n])
	if !ok || len(bodies) != 1 || !validPartyLegacySkillBody(bodies[0], 5) || bodies[0][11]^0x7e != 0 || !r.partySkillRecoverAt.IsZero() {
		t.Fatalf("acknowledged attack did not recover to stand: %x", buf[:n])
	}
}

func TestNormalAttackRequiresSupportedActorAndCurrentEnemyInRange(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name   string
		change func(*RobotVo, [4]byte)
	}{
		{"other-job", func(r *RobotVo, e [4]byte) { r.partySkillJob = 1 }},
		{"unknown-profile", func(r *RobotVo, e [4]byte) { r.partySkillLoaded = false }},
		{"cooling", func(r *RobotVo, e [4]byte) { r.partyBasicAttackNextAt = now.Add(time.Second) }},
		{"dead-target", func(r *RobotVo, e [4]byte) {
			a := r.partyDungeonWorld.actors[e]
			a.dead = true
			r.partyDungeonWorld.actors[e] = a
		}},
		{"unknown-target", func(r *RobotVo, e [4]byte) {
			a := r.partyDungeonWorld.actors[e]
			a.position.known = 1
			r.partyDungeonWorld.actors[e] = a
		}},
		{"out-of-range", func(r *RobotVo, e [4]byte) {
			a := r.partyDungeonWorld.actors[e]
			a.position.x = 250
			r.partyDungeonWorld.actors[e] = a
		}},
		{"wrong-lane", func(r *RobotVo, e [4]byte) {
			a := r.partyDungeonWorld.actors[e]
			a.position.y = 340
			r.partyDungeonWorld.actors[e] = a
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, enemy := basicAttackTestRobot(t, now)
			tc.change(r, enemy)
			if r.tryPartyBasicAttackUnsafe(nil, now) || !r.partySkillRecoverAt.IsZero() {
				t.Fatal("normal attack bypassed actor, target or cadence requirements")
			}
		})
	}
}

func TestFailedNormalAttackDoesNotCommitCadenceAndRoomResetClearsIt(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	sender.Close()
	r.partyPeers[0].outerIP = net.IPv4(127, 0, 0, 1)
	r.partyPeers[0].port = 5063
	if r.tryPartyBasicAttackUnsafe(sender, now) || !r.partyBasicAttackNextAt.IsZero() || !r.partySkillRecoverAt.IsZero() {
		t.Fatal("failed send committed attack timing")
	}
	r.partyBasicAttackNextAt = now.Add(time.Second)
	r.partyBasicAttackStep, r.partyBasicAttackTarget = 2, [4]byte{0x11, 2, 1, 3}
	r.clearPartyDungeonRuntimeUnsafe()
	if !r.partyBasicAttackNextAt.IsZero() || r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} {
		t.Fatal("room reset retained attack cadence")
	}
	r.partyBasicAttackNextAt = now.Add(time.Second)
	r.partyBasicAttackStep, r.partyBasicAttackTarget = 2, [4]byte{0x11, 2, 1, 3}
	r.resetPartyTQOSTransportUnsafe()
	if !r.partyBasicAttackNextAt.IsZero() || r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} {
		t.Fatal("transport reset retained attack cadence")
	}
}

func comboTestTransport(t *testing.T, r *RobotVo) (*net.UDPConn, *net.UDPConn) {
	t.Helper()
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { receiver.Close() })
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sender.Close() })
	remote := receiver.LocalAddr().(*net.UDPAddr)
	r.partyPeers[0].outerIP, r.partyPeers[0].port = remote.IP, uint16(remote.Port)
	return sender, receiver
}

func readComboState(t *testing.T, receiver *net.UDPConn, state, step byte, records int) {
	t.Helper()
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	bodies, ok := partyDungeonFrameBodies(buf[:n])
	if !ok || len(bodies) != records {
		t.Fatalf("unexpected frame: %x", buf[:n])
	}
	body := bodies[len(bodies)-1]
	if !validPartyLegacySkillBody(body, 5) || body[11]^0x7e != state || state == 8 && (len(body) != 17 || body[14]^0x7e != step) {
		t.Fatalf("unexpected combo state: %x", body)
	}
}

func TestMaleMageFourStageComboStaysOrderedAndRecoversOnce(t *testing.T) {
	now := time.Now()
	r, _, enemy := basicAttackTestRobot(t, now)
	r.partySkillJob = 8
	// A silent living enemy remains a valid target throughout the combo.
	actor := r.partyDungeonWorld.actors[enemy]
	actor.seenAt = now.Add(-10 * time.Second)
	r.partyDungeonWorld.actors[enemy] = actor
	sender, receiver := comboTestTransport(t, r)
	for step := byte(0); step < 4; step++ {
		r.flushPartyDungeonSkillUnsafe(sender, now)
		readComboState(t, receiver, 8, step, 2)
		if len(r.partySkillReadyAt) != 0 || r.partySkillRecoverAt != now.Add(partySkillRecoverDelay) {
			t.Fatal("combo consumed an active-skill cooldown or lost recovery")
		}
		if step < 3 && (r.partyBasicAttackStep != step+1 || r.partyBasicAttackTarget != enemy) {
			t.Fatal("combo lost its target or stage")
		}
		r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, uint32(step)), 1, r.partyPeers[0])
		if step < 3 {
			sequence := r.partyTQOSReliableSeq[0][1]
			r.flushPartyDungeonSkillUnsafe(sender, now.Add(partyBasicComboInterval-time.Nanosecond))
			if r.partyTQOSReliableSeq[0][1] != sequence {
				t.Fatal("combo sent before its cadence")
			}
			now = now.Add(partyBasicComboInterval)
		}
	}
	if r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} || r.partyBasicAttackNextAt != now.Add(partyBasicAttackInterval) {
		t.Fatal("completed combo did not release its target and schedule its next start")
	}
	r.flushPartyDungeonSkillUnsafe(sender, now.Add(partySkillRecoverDelay))
	readComboState(t, receiver, 0, 0, 1)
	if !r.partySkillRecoverAt.IsZero() {
		t.Fatal("combo did not recover after its final stage")
	}
}

func TestMaleMageEnvelopeComboMatchesRealClientVectors(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.partySkillJob, r.partyDungeonLegacySkills = 8, false
	sender, receiver := comboTestTransport(t, r)
	for step := byte(0); step < 4; step++ {
		r.flushPartyDungeonSkillUnsafe(sender, now)
		buf := make([]byte, 4096)
		receiver.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := receiver.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		bodies, ok := partyDungeonFrameBodies(buf[:n])
		if !ok || len(bodies) != 2 {
			t.Fatalf("missing ordered facing and attack: %x", buf[:n])
		}
		command, payload, ok := decodePartyDungeonEnvelope(bodies[1])
		// The real male-mage capture has actor/state=8/length=1/step=0..3/token.
		if !ok || command != 5 || len(payload) != 10 || binary.LittleEndian.Uint32(payload) != 0x00050111 || payload[4] != 8 || binary.LittleEndian.Uint16(payload[5:7]) != 1 || payload[7] != step {
			t.Fatalf("invalid modern attack vector: %x", bodies[1])
		}
		r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, uint32(step)), 1, r.partyPeers[0])
		now = now.Add(partyBasicComboInterval)
	}
}

func TestMaleMageComboDoesNotAdvanceBeforeAckOrAfterFailedSend(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.partySkillJob = 8
	sender, receiver := comboTestTransport(t, r)
	r.flushPartyDungeonSkillUnsafe(sender, now)
	readComboState(t, receiver, 8, 0, 2)
	next, recoverAt := r.partyBasicAttackNextAt, r.partySkillRecoverAt
	r.flushPartyDungeonSkillUnsafe(sender, next)
	if r.partyBasicAttackStep != 1 || r.partyTQOSReliableSeq[0][1] != 1 || r.partySkillRecoverAt != recoverAt {
		t.Fatal("combo advanced without acknowledgement")
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
	sender.Close()
	r.flushPartyDungeonSkillUnsafe(sender, next)
	if r.partyBasicAttackStep != 1 || r.partyBasicAttackNextAt != next || r.partySkillRecoverAt != recoverAt {
		t.Fatal("failed combo send committed its next stage or timing")
	}
}

func TestMaleMageComboCannotCarryAcrossTargetLossOrInterruption(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RobotVo, [4]byte, time.Time) time.Time
	}{
		{"dead", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time {
			a := r.partyDungeonWorld.actors[enemy]
			a.dead = true
			r.partyDungeonWorld.actors[enemy] = a
			return now.Add(partyBasicComboInterval)
		}},
		{"changed-target", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time {
			a := r.partyDungeonWorld.actors[enemy]
			delete(r.partyDungeonWorld.actors, enemy)
			r.partyDungeonWorld.actors[[4]byte{0x11, 2, 2, 3}] = a
			return now.Add(partyBasicComboInterval)
		}},
		{"unknown-target", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time {
			a := r.partyDungeonWorld.actors[enemy]
			a.position.known = 1
			r.partyDungeonWorld.actors[enemy] = a
			return now.Add(partyBasicComboInterval)
		}},
		{"out-of-range", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time {
			a := r.partyDungeonWorld.actors[enemy]
			a.position.x += 500
			r.partyDungeonWorld.actors[enemy] = a
			return now.Add(partyBasicComboInterval)
		}},
		{"late", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time { return now.Add(partySkillRecoverDelay) }},
		{"blocked", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time {
			r.partySkillBlockedUntil = now.Add(time.Minute)
			return now.Add(partyBasicComboInterval)
		}},
		{"inactive-room", func(r *RobotVo, enemy [4]byte, now time.Time) time.Time {
			r.partyDungeonLastAt = now.Add(-time.Minute)
			return now.Add(partyBasicComboInterval)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			r, _, enemy := basicAttackTestRobot(t, now)
			r.partySkillJob = 8
			sender, receiver := comboTestTransport(t, r)
			r.flushPartyDungeonSkillUnsafe(sender, now)
			readComboState(t, receiver, 8, 0, 2)
			r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
			r.flushPartyDungeonSkillUnsafe(sender, tc.change(r, enemy, now))
			if r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} {
				t.Fatal("partial combo survived target loss or interruption")
			}
			if tc.name == "late" {
				readComboState(t, receiver, 0, 0, 1)
			} else if r.partyTQOSReliableSeq[0][1] != 1 {
				t.Fatal("interrupted combo sent another attack")
			}
		})
	}
}

func TestDisabledPartyCombatDoesNotSendBasicOrActiveAttack(t *testing.T) {
	for _, job := range []int{0, 3, 8} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("job%d/active%t", job, active), func(t *testing.T) {
				now := time.Now()
				r, _, _ := basicAttackTestRobot(t, now)
				shared.SetPartySkillStates(nil) // The published top-level off switch.
				r.partySkillJob = job
				if active {
					r.partySkillNextAt = now
					r.partySkillCandidates = []partySkillCandidate{{itemCostFree: true, skillIndex: 12, state: 23, stateData: []byte{12}}}
				}
				sender, receiver := comboTestTransport(t, r)
				r.flushPartyDungeonSkillUnsafe(sender, now)
				receiver.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
				if n, _, err := receiver.ReadFromUDP(make([]byte, 4096)); err == nil {
					t.Fatalf("disabled combat sent %d bytes", n)
				}
			})
		}
	}
}

func TestFemaleMageThreeStageComboFromOriginalClient(t *testing.T) {
	now := time.Now()
	r, _, enemy := basicAttackTestRobot(t, now)
	r.partySkillJob = 3
	sender, receiver := comboTestTransport(t, r)
	for step := byte(0); step < 3; step++ {
		r.flushPartyDungeonSkillUnsafe(sender, now)
		readComboState(t, receiver, 8, step, 2)
		r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, uint32(step)), 1, r.partyPeers[0])
		if step < 2 {
			if r.partyBasicAttackTarget != enemy || r.partyBasicAttackStep != step+1 {
				t.Fatal("female mage lost the current combo target")
			}
			now = r.partyBasicAttackNextAt
		}
	}
	if r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} {
		t.Fatal("female mage continued beyond its three PVF attack motions")
	}
	r.flushPartyDungeonSkillUnsafe(sender, now.Add(partySkillRecoverDelay))
	readComboState(t, receiver, 0, 0, 1)
}
