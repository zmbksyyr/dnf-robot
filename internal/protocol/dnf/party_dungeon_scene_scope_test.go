package dnf

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"testing"
	"time"
)

// Constructed motion inputs use the original room-grid fields. They are not
// additional real-client captures.
func observeDungeonRoomMotion(t *testing.T, world *partyDungeonWorld, ref [4]byte, room [2]byte, x, y float32, now time.Time) {
	t.Helper()
	entry := partyDungeonMotion{entity: ref, controller: ref, flags: [3]byte{room[0], room[1], 0x10}, modes: 0x555555}
	for i, value := range []float32{x, y} {
		entry.modes &^= 3 << uint((9+i)*2)
		entry.values[9+i] = math.Float32bits(value)
	}
	body, ok := buildPartyDungeonMotion([]partyDungeonMotion{entry})
	if !ok {
		t.Fatal("motion encoding failed")
	}
	world.observe(body, now)
}

func TestDungeonTargetsStayInObservedOwnerRoom(t *testing.T) {
	for _, room := range [][2]byte{{0, 0}, {1, 2}} {
		t.Run(fmt.Sprint(room), func(t *testing.T) {
			now := time.Unix(100, 0)
			world := partyDungeonWorld{}
			self := partyDungeonPlayerReference(5)
			old, current := [4]byte{0x11, 2, 1, 3}, [4]byte{0x11, 2, 2, 3}
			otherRoom := [2]byte{room[0] + 1, room[1]}
			observeDungeonRoomMotion(t, &world, self, room, 100, 300, now)
			observeDungeonRoomMotion(t, &world, old, otherRoom, 101, 300, now)
			observeDungeonRoomMotion(t, &world, current, room, 500, 300, now)
			position := world.actors[self].position
			for _, cached := range [][4]byte{{}, old} {
				world.target = cached
				ref, _, ok := world.nearestTarget(self, position)
				if !ok || ref != current {
					t.Fatalf("target=%x cached=%x: other-room actor entered search", ref, cached)
				}
			}
			world.noteTargetAttack(current, now)
			world.noteTargetAttack(current, now.Add(16*time.Second))
			world.reconsiderTarget(self, position, now.Add(16*time.Second))
			if world.target != current || world.actors[old].dead || world.actors[old].hpKnown {
				t.Fatal("no-progress rotation selected or fabricated death of another room's actor")
			}
			// An observed owner-room change must invalidate a cached target even
			// before its next movement record arrives.
			observeDungeonRoomMotion(t, &world, self, otherRoom, 100, 300, now.Add(17*time.Second))
			if ref, _, ok := world.nearestTarget(self, position); !ok || ref != old {
				t.Fatalf("owner-room update did not revalidate cached target: %x", ref)
			}
		})
	}
}

func TestDungeonCombatWaitsForMatchingRooms(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, attack := range []string{"basic", "active"} {
			for _, pending := range []string{"old-target", "leader-ahead"} {
				t.Run(fmt.Sprintf("legacy=%t/%s/%s", legacy, attack, pending), func(t *testing.T) {
					now := time.Now()
					r, self, enemy := basicAttackTestRobot(t, now)
					r.partySkillJob, r.partyDungeonLegacySkills = 8, legacy
					r.partySkillCandidates = []partySkillCandidate{{itemCostFree: true, skillIndex: 3, state: 22, stateData: []byte{3, 0, 0}, cooldown: time.Second}}
					sender, receiver := comboTestTransport(t, r)
					room, otherRoom := [2]byte{1, 2}, [2]byte{1, 1}
					leader := partyDungeonPlayerReference(r.partyPeers[0].uniqueID)
					observeDungeonRoomMotion(t, &r.partyDungeonWorld, self, room, 100, 300, now)
					observeDungeonRoomMotion(t, &r.partyDungeonWorld, leader, room, 500, 300, now)
					observeDungeonRoomMotion(t, &r.partyDungeonWorld, enemy, room, 30, 300, now)
					changed := enemy
					if pending == "leader-ahead" {
						changed = leader
					}
					before := r.partyDungeonWorld.actors[changed].position
					observeDungeonRoomMotion(t, &r.partyDungeonWorld, changed, otherRoom, before.x, before.y, now)
					r.partyDungeonWorld.target = enemy
					if attack == "basic" {
						r.partyBasicAttackStep, r.partyBasicAttackTarget = 1, enemy
					}
					invoke := func() {
						if attack == "basic" {
							r.tryPartyBasicAttackUnsafe(sender, now)
						} else {
							r.partySkillNextAt = now
							r.flushPartyDungeonSkillUnsafe(sender, now)
						}
					}
					invoke()
					if r.partyTQOSReliableSeq[0][1] != 0 || !r.partySkillRecoverAt.IsZero() || len(r.partySkillReadyAt) != 0 || !r.partyDungeonWorld.targetAttackAt.IsZero() {
						t.Fatal("room mismatch consumed a cast, cooldown or transport sequence")
					}
					if r.partyBasicAttackStep != 0 || r.partyBasicAttackTarget != [4]byte{} {
						t.Fatal("room mismatch retained a pending combo")
					}
					receiver.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
					buf := make([]byte, 4096)
					if _, _, err := receiver.ReadFromUDP(buf); err == nil {
						t.Fatal("room mismatch emitted a combat frame")
					} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
						t.Fatal(err)
					}
					observeDungeonRoomMotion(t, &r.partyDungeonWorld, changed, room, before.x, before.y, now)
					invoke()
					receiver.SetReadDeadline(time.Now().Add(time.Second))
					n, _, err := receiver.ReadFromUDP(buf)
					if err != nil {
						t.Fatal(err)
					}
					bodies, ok := partyDungeonFrameBodies(buf[:n])
					if !ok || len(bodies) != 2 {
						t.Fatalf("resumed combat frame=%x", buf[:n])
					}
					motion, ok := decodePartyDungeonMotion(bodies[0])
					if !ok || len(motion) != 1 || motion[0].flags[0] != room[0] || motion[0].flags[1] != room[1] || motion[0].entity != self {
						t.Fatal("resumed attack changed the owner's room")
					}
					wantCommand := uint16(partyDungeonEnvelopeCommand)
					if legacy {
						wantCommand = 5
					}
					if binary.LittleEndian.Uint16(bodies[1][1:3]) != wantCommand || r.partyDungeonWorld.targetAttackAt != now {
						t.Fatal("resumed cast lost its protocol or combat tracking")
					}
				})
			}
		}
	}
}

func TestDungeonSceneScopeIncludesObjectives(t *testing.T) {
	now := time.Unix(100, 0)
	world, self, _, enemy := navigationTestWorld(now)
	objective := [4]byte{0x21, 0, 2, 0}
	world.actors[objective] = partyDungeonActor{objective: true}
	observeDungeonRoomMotion(t, &world, objective, [2]byte{1, 1}, 101, 300, now)
	world.target = objective
	if ref, _, ok := world.nearestTarget(self, world.actors[self].position); !ok || ref != enemy {
		t.Fatal("another room's objective bypassed the scene scope")
	}
	observeDungeonRoomMotion(t, &world, objective, world.actors[self].room, 101, 300, now)
	world.target = [4]byte{}
	if ref, _, ok := world.nearestTarget(self, world.actors[self].position); !ok || ref != objective {
		t.Fatal("current-room objective became ineligible")
	}
}

func TestDungeonCombatUsesOwnerRoomWithoutLeaderMotion(t *testing.T) {
	now := time.Now()
	r, self, _ := basicAttackTestRobot(t, now)
	leader := partyDungeonPlayerReference(r.partyPeers[0].uniqueID)
	delete(r.partyDungeonWorld.actors, leader)
	// Team/HP events can create an actor without any motion or room fields.
	observeDungeonTeam(&r.partyDungeonWorld, leader, 0, now)
	sender, receiver := comboTestTransport(t, r)
	if !r.tryPartyBasicAttackUnsafe(sender, now) {
		t.Fatal("non-motion leader metadata invented a conflicting room")
	}
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	bodies, ok := partyDungeonFrameBodies(buf[:n])
	if !ok || len(bodies) != 2 {
		t.Fatal("missing facing and attack")
	}
	motion, ok := decodePartyDungeonMotion(bodies[0])
	room := r.partyDungeonWorld.actors[self].room
	if !ok || len(motion) != 1 || motion[0].flags[0] != room[0] || motion[0].flags[1] != room[1] {
		t.Fatal("missing leader motion changed the owner's room to a zero default")
	}
}

func TestDungeonSceneScopeAcceptsCapturedPlayerAndMonsterMotion(t *testing.T) {
	// Same real session, grid (1,0): support-protocol1/udp-observed-casts4.jsonl.
	// These unmodified records establish that both actor kinds carry the room
	// fields; a monster's controller is the player, not its own actor reference.
	now := time.Unix(100, 0)
	world := partyDungeonWorld{}
	for _, wire := range []string{
		"010400ec17c8957f6f7f7f7e6f7f7f7e7f7e4e2b2b3f7efefb3a7efee13d",
		"0104009a24a3977f6f7c7fbf6f7f7f7e7f7e6e012f3f7e3e493a7e7e2a3d5a848181",
	} {
		world.observe(mustPartyHex(t, wire), now)
	}
	self, monster := partyDungeonPlayerReference(1), [4]byte{0x11, 2, 1, 0xc1}
	if world.actors[self].room != [2]byte{1, 0} || world.actors[monster].room != [2]byte{1, 0} {
		t.Fatal("room field interpretation differs between captured actor kinds")
	}
	if ref, _, ok := world.nearestTarget(self, world.actors[self].position); !ok || ref != monster {
		t.Fatal("scene scope excluded the captured same-room monster")
	}
}
