package dnf

import (
	"testing"
	"time"
)

func TestMonsterAttackConsumesHPAndIdleRecoveryRestoresResources(t *testing.T) {
	now := time.Unix(100, 0)
	r, self, enemy := basicAttackTestRobot(t, now)
	r.characterResources = characterResourcePool{known: true, hp: 180, mp: 100, maxHP: 180, maxMP: 140, recoveredAt: now}
	r.seedPartyResourcesUnsafe()
	// A received monster attack, not the Robot's own attack attempt, causes HP loss.
	r.partyDungeonWorld.noteTargetAttack(enemy, now)
	r.simulatePartyHurtUnsafe(now)
	if r.characterResources.hp != 180 {
		t.Fatal("outgoing attack consumed own HP")
	}
	body := buildPartyLegacySkillStateBody(uint16(enemy[2])|uint16(enemy[3])<<8, 8, []byte{0}, 1)
	// The native monster uses the same state tuple with monster group 0x0211.
	plain := append([]byte(nil), body[7:]...)
	for i := range plain {
		plain[i] ^= 0x7e
	}
	copy(plain[:4], enemy[:])
	checksum := partyPayloadChecksum(plain)
	copy(body[3:7], checksum[:])
	for i, v := range plain {
		body[7+i] = v ^ 0x7e
	}
	r.partyDungeonWorld.observe(body, now)
	r.simulatePartyHurtUnsafe(now)
	if r.characterResources.hp >= 180 || r.partyDungeonWorld.actors[self].hp != r.characterResources.hp {
		t.Fatal("received nearby attack did not consume HP")
	}
	after := r.characterResources.hp
	r.simulatePartyHurtUnsafe(now.Add(800 * time.Millisecond))
	if r.characterResources.hp != after {
		t.Fatal("same attack paid twice")
	}
	// Leaving the room removes combat actors but preserves the resource pool.
	r.clearPartyDungeonRuntimeUnsafe()
	r.flushPartyResourcesUnsafe(nil, now.Add(100*time.Second))
	if r.characterResources.hp != 180 || r.characterResources.mp != 140 {
		t.Fatalf("idle recovery did not fill the pool: %+v", r.characterResources)
	}
}
