package dnf

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"robot/internal/shared"
)

func TestAutoSkillCandidatesRequireKnownVectorAndKeepObservedEmpty(t *testing.T) {
	entries := []shared.PartySkillState{
		{Job: 8, SkillIndex: 1, State: 20, StateData: []byte{}},
		{Job: 8, SkillIndex: 26, State: 43, StateData: nil},
	}
	pvfStates := []shared.SkillState{
		{Job: 8, SkillIndex: 1, State: 20, ItemCostFree: true},
		{Job: 8, SkillIndex: 26, State: 43, ItemCostFree: true},
	}
	candidates, stats := partySkillCandidatesFromCatalogMode(true, 8, entries, pvfStates)
	if len(candidates) != 1 || candidates[0].skillIndex != 1 || candidates[0].stateData == nil || len(candidates[0].stateData) != 0 {
		t.Fatalf("auto candidates lost the observed empty vector or kept unknown data: %+v", candidates)
	}
	if stats.PVFMatched != 1 || stats.SkippedUnknownStateData != 1 {
		t.Fatalf("candidate stats = %+v", stats)
	}
}

func TestNativeInt32VectorSurvivesRuntimeAndCastTransport(t *testing.T) {
	// This diagnostic vector is encoded/decoded by the original client in
	// offline scratch. It is only sent to a local test UDP socket, not executed
	// as a skill or claimed as a valid parameter list for any profession.
	want := []byte{0x87, 0xff, 0xff, 0xff, 0x7f, 0x8f, 0xff, 0xff, 0xff, 0x7f, 0x88, 0x80, 0x80, 0x80, 0, 0x8f, 0xff, 0xff, 0xff, 1}
	// The catalog-to-shared boundary is checked in the catalog package with
	// this same native oracle. Protocol consumes only the shared contract.
	entries := []shared.PartySkillState{{Job: 8, SkillIndex: 2, State: 25, Level: 1, ScriptPath: "sqr/test.nut", StateData: bytes.Clone(want)}}
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "envelope", true: "legacy"}[legacy], func(t *testing.T) {
			now := time.Now()
			r, _, _ := basicAttackTestRobot(t, now)
			r.State, r.CID = StateRun, 1
			r.partyDungeonLegacySkills = legacy
			r.partyBasicAttackNextAt = now.Add(time.Hour)
			r.partySkillLevels[2] = 1
			candidates, _ := partySkillCandidatesFromCatalogMode(true, 8, entries,
				[]shared.SkillState{{Job: 8, SkillIndex: 2, State: 25, ScriptPath: "sqr/test.nut", ItemCostFree: true}})
			r.loadPartySkillProfile(0, shared.PartySkillStateGeneration(), r.UID, r.CID, func(uint32, int) (partySkillProfile, error) {
				return partySkillProfile{cid: 1, job: 8, candidates: candidates}, nil
			})
			r.partySkillNextAt = now
			sender, receiver := comboTestTransport(t, r)
			r.flushPartyDungeonSkillUnsafe(sender, now)
			receiver.SetReadDeadline(time.Now().Add(time.Second))
			buf := make([]byte, 4096)
			n, _, err := receiver.ReadFromUDP(buf)
			if err != nil {
				t.Fatal(err)
			}
			bodies, ok := partyDungeonFrameBodies(buf[:n])
			if !ok || len(bodies) != 2 {
				t.Fatalf("missing skill transport: %x", buf[:n])
			}
			body := bodies[len(bodies)-1]
			var payload []byte
			if legacy {
				if !validPartyLegacySkillBody(body, r.partySelfPeer.uniqueID) {
					t.Fatalf("invalid direct state: %x", body)
				}
				payload = append([]byte(nil), body[7:]...)
				for i := range payload {
					payload[i] ^= 0x7e
				}
			} else {
				command, data, valid := decodePartyDungeonEnvelope(body)
				if !valid || command != 5 {
					t.Fatalf("invalid envelope: %x", body)
				}
				payload = data
			}
			if payload[4] != 25 || int(binary.LittleEndian.Uint16(payload[5:7])) != len(want) || !bytes.Equal(payload[7:len(payload)-2], want) {
				t.Fatalf("vector changed at cast transport: %x", payload)
			}
		})
	}
}

func TestOversizedSkillVectorDoesNotReachCastTransport(t *testing.T) {
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.partySkillJob = 8
	sender, _ := comboTestTransport(t, r)
	for _, size := range []int{2000, shared.MaxPartySkillStateDataSize + 1} {
		if r.sendPartySkillStateUnsafe(sender, now, 25, make([]byte, size), "CAST") {
			t.Fatalf("oversized vector reached transport: %d", size)
		}
		if r.partyTQOSReliableSeq[0][1] != 0 {
			t.Fatal("rejected vector advanced the reliable sequence")
		}
	}
}
