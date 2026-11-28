package dnf

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestObservedResourcesAreAuthoritativeAndNoSyntheticRecovery(t *testing.T) {
	now := time.Unix(100, 0)
	r, self, _ := basicAttackTestRobot(t, now)
	r.partyDungeonWorld.observe(buildPartyResourceBody(12, r.partySelfPeer.uniqueID, 180), now)
	r.rememberPartyResourcesUnsafe()
	if got := r.characterResources.hp; got != 180 {
		t.Fatalf("initial HP=%d, want 180", got)
	}
	mp := make([]byte, 8)
	copy(mp, self[:])
	binary.LittleEndian.PutUint32(mp[4:], 77)
	r.partyDungeonWorld.observeActorEvent(13, mp, now)
	r.rememberPartyResourcesUnsafe()
	if r.characterResources.mp != 77 || !r.characterResources.mpObserved {
		t.Fatalf("observed MP was not retained: %+v", r.characterResources)
	}
	r.flushPartyResourcesUnsafe(nil, now.Add(100*time.Second))
	if r.characterResources.mp != 77 {
		t.Fatalf("idle flush synthesized MP recovery: %+v", r.characterResources)
	}
	hp := make([]byte, 44)
	copy(hp[4:8], self[:])
	binary.LittleEndian.PutUint32(hp[30:], 123)
	r.partyDungeonWorld.observeActorEvent(20, hp, now.Add(time.Second))
	r.rememberPartyResourcesUnsafe()
	if r.characterResources.hp != 123 || r.partyDungeonWorld.actors[self].hp != 123 {
		t.Fatalf("observed HP damage was not retained: %+v", r.characterResources)
	}
	r.flushPartyResourcesUnsafe(nil, now.Add(101*time.Second))
	if r.characterResources.hp != 123 {
		t.Fatalf("idle flush synthesized HP recovery: %+v", r.characterResources)
	}
}

func TestResourceSyncSendsOnlyObservedValuesAndRetriesFailedSend(t *testing.T) {
	for _, command := range []byte{12, 13} {
		t.Run(map[byte]string{12: "HP only", 13: "MP only"}[command], func(t *testing.T) {
			now := time.Now()
			r, self, _ := basicAttackTestRobot(t, now)
			r.partyDungeonWorld.observe(buildPartyResourceBody(command, r.partySelfPeer.uniqueID, 77), now)
			r.rememberPartyResourcesUnsafe()
			sender, _ := comboTestTransport(t, r)
			sender.Close()
			r.flushPartyResourcesUnsafe(sender, now)
			if !r.characterResources.syncPending {
				t.Fatal("failed sync lost the pending observation")
			}
			sender, receiver := comboTestTransport(t, r)
			// The transport backs off a failed route for two seconds.
			r.flushPartyResourcesUnsafe(sender, now.Add(3*time.Second))
			buf := make([]byte, 4096)
			receiver.SetReadDeadline(time.Now().Add(time.Second))
			n, _, err := receiver.ReadFromUDP(buf)
			if err != nil {
				t.Fatal(err)
			}
			bodies, ok := partyDungeonFrameBodies(buf[:n])
			if !ok || len(bodies) != 1 || bodies[0][1] != command {
				t.Fatalf("sync invented the other resource: %x", buf[:n])
			}
			world := partyDungeonWorld{}
			world.observe(bodies[0], now)
			actor := world.actors[self]
			if command == 12 && (!actor.hpKnown || actor.hp != 77 || actor.mpKnown) || command == 13 && (!actor.mpKnown || actor.mp != 77 || actor.hpKnown) {
				t.Fatalf("synced resource differs from observation: %+v", actor)
			}
			r.rememberPartyResourcesUnsafe()
			if r.characterResources.syncPending {
				t.Fatal("unchanged state scheduled a duplicate resource sync")
			}
			r.markPartyDungeonEnteredUnsafe(now.Add(4 * time.Second))
			if !r.characterResources.syncPending {
				t.Fatal("room entry did not schedule retained resources")
			}
		})
	}
}
