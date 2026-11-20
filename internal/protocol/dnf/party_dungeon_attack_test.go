package dnf

import (
	"net"
	"testing"
	"time"

	"robot/internal/shared"
)

func basicAttackTestRobot(now time.Time) (*RobotVo, [4]byte, [4]byte) {
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
	}{{"swordman", 0}, {"male-mage", 8}} {
		t.Run(tc.name, func(t *testing.T) {
			testNormalAttackFacesOwnTargetAndWaitsForAcknowledgementBeforeRecovery(t, tc.job)
		})
	}
}

func testNormalAttackFacesOwnTargetAndWaitsForAcknowledgementBeforeRecovery(t *testing.T, job int) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(now)
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
	if r.partyBasicAttackNextAt != now.Add(partyBasicAttackInterval) || r.partySkillRecoverAt != now.Add(partySkillRecoverDelay) || len(r.partySkillReadyAt) != 0 || len(r.partyDungeonFollow) != 0 {
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
		{"unknown-wire", func(r *RobotVo, e [4]byte) { r.partyDungeonLegacySkills = false }},
		{"cooling", func(r *RobotVo, e [4]byte) { r.partyBasicAttackNextAt = now.Add(time.Second) }},
		{"dead-target", func(r *RobotVo, e [4]byte) {
			a := r.partyDungeonWorld.actors[e]
			a.dead = true
			r.partyDungeonWorld.actors[e] = a
		}},
		{"stale-target", func(r *RobotVo, e [4]byte) {
			a := r.partyDungeonWorld.actors[e]
			a.seenAt = now.Add(-4 * time.Second)
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
			r, _, enemy := basicAttackTestRobot(now)
			tc.change(r, enemy)
			if r.tryPartyBasicAttackUnsafe(nil, now) || !r.partySkillRecoverAt.IsZero() {
				t.Fatal("normal attack bypassed actor, target or cadence requirements")
			}
		})
	}
}

func TestFailedNormalAttackDoesNotCommitCadenceAndRoomResetClearsIt(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(now)
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
	r.clearPartyDungeonRuntimeUnsafe()
	if !r.partyBasicAttackNextAt.IsZero() {
		t.Fatal("room reset retained attack cadence")
	}
	r.partyBasicAttackNextAt = now.Add(time.Second)
	r.resetPartyTQOSTransportUnsafe()
	if !r.partyBasicAttackNextAt.IsZero() {
		t.Fatal("transport reset retained attack cadence")
	}
}
