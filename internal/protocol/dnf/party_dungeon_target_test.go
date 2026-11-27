package dnf

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestDungeonRepeatedMissesTryOtherEnemiesAndRetainOriginalTarget(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	other := [4]byte{0x11, 2, 2, 3}
	world.actors[other] = partyDungeonActor{position: partyDungeonPosition{x: 600, y: 300, known: 7}, room: world.actors[self].room}
	world.planNavigation(self, leader, now)
	for i := 0; i < 15; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		world.noteTargetAttack(enemy, at)
		world.planNavigation(self, leader, at)
		if world.target != enemy {
			t.Fatalf("changed target before allowing sustained combat: %x", world.target)
		}
	}
	world.noteTargetAttack(enemy, now.Add(15*time.Second))
	world.planNavigation(self, leader, now.Add(15*time.Second))
	if world.target != other || world.navigation.mode != "chase" {
		t.Fatalf("repeated misses did not try the other enemy: %x", world.target)
	}
	actor := world.actors[enemy]
	if actor.dead || actor.hpKnown || actor.position.x != 300 {
		t.Fatalf("priority change fabricated death, HP or position: %+v", actor)
	}
	// A real HP-zero report for the alternative must make the original enemy
	// eligible again. A timeout must never turn into a clear-room decision.
	plain := mustPartyHex(t, dungeonDamageCaptures[2].plain)
	copy(plain[4:8], other[:])
	world.observe(dungeonDamageBody(t, hex.EncodeToString(plain)), now.Add(16*time.Second))
	world.planNavigation(self, leader, now.Add(16*time.Second))
	if world.target != enemy || world.navigation.mode != "chase" {
		t.Fatal("unconfirmed original enemy disappeared after the alternative died")
	}
}

func TestDungeonTargetProgressRequiresNewHPReduction(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, enemy := navigationTestWorld(now)
	other := [4]byte{0x11, 2, 2, 3}
	world.actors[other] = partyDungeonActor{position: partyDungeonPosition{x: 600, y: 300, known: 7}, room: world.actors[self].room}
	world.planNavigation(self, leader, now)
	for second := 0; second <= 30; second++ {
		at := now.Add(time.Duration(second) * time.Second)
		world.noteTargetAttack(enemy, at)
		// The second valid hit at t=10 extends combat. Repeating its same HP
		// afterwards is traffic, not continued progress.
		if second == 1 {
			world.observe(dungeonDamageBody(t, dungeonDamageCaptures[0].plain), at)
		}
		if second >= 10 {
			world.observe(dungeonDamageBody(t, dungeonDamageCaptures[1].plain), at)
		}
		if second == 24 {
			corrupt := dungeonDamageBody(t, dungeonDamageCaptures[2].plain)
			corrupt[7] ^= 1
			world.observe(corrupt, at)
		}
		world.planNavigation(self, leader, at)
		want := enemy
		if second >= 25 {
			want = other
		}
		if world.target != want {
			t.Fatalf("at %ds target=%x want=%x", second, world.target, want)
		}
	}
}

func TestDungeonTargetRotationNeedsRecentAttacksAndAnAlternative(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []string{"no-attacks", "attacks-stopped", "only-enemy"} {
		t.Run(tc, func(t *testing.T) {
			world, self, leader, enemy := navigationTestWorld(now)
			if tc != "only-enemy" {
				world.actors[[4]byte{0x11, 2, 2, 3}] = partyDungeonActor{position: partyDungeonPosition{x: 600, y: 300, known: 7}, room: world.actors[self].room}
			}
			world.planNavigation(self, leader, now)
			if tc != "no-attacks" {
				world.noteTargetAttack(enemy, now)
			}
			if tc == "only-enemy" {
				world.noteTargetAttack(enemy, now.Add(time.Minute))
			}
			world.planNavigation(self, leader, now.Add(time.Minute))
			if world.target != enemy || world.actors[enemy].dead || world.navigation.mode != "chase" {
				t.Fatal("silence, travel or an absent alternative invalidated a live target")
			}
		})
	}
}

func TestDungeonUnproductiveTargetsDoNotHideUntriedEnemies(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, leader, first := navigationTestWorld(now)
	second, third := [4]byte{0x11, 2, 2, 3}, [4]byte{0x11, 2, 3, 3}
	world.actors[second] = partyDungeonActor{position: partyDungeonPosition{x: 310, y: 300, known: 7}, room: world.actors[self].room}
	world.actors[third] = partyDungeonActor{position: partyDungeonPosition{x: 1000, y: 300, known: 7}, room: world.actors[self].room}
	world.planNavigation(self, leader, now)
	for i, ref := range [][4]byte{first, second, third, first} {
		if world.target != ref {
			t.Fatalf("round=%d target=%x want=%x", i, world.target, ref)
		}
		start := now.Add(time.Duration(i) * 16 * time.Second)
		world.noteTargetAttack(ref, start)
		world.noteTargetAttack(ref, start.Add(15*time.Second))
		world.planNavigation(self, leader, start.Add(15*time.Second))
	}
}
