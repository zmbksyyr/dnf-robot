package dnf

import (
	"robot/internal/shared"
	"testing"
	"time"
)

func setPartyCombatTestEnabled(t *testing.T) {
	t.Helper()
	previousEnabled, previousAuto := shared.PartySkillEnabled(), shared.PartySkillAutoSelection()
	var previous []shared.PartySkillState
	for job := 0; job <= 10; job++ {
		previous = append(previous, shared.PartySkillStatesForJob(job)...)
	}
	shared.SetPartySkillRuntime(previous, true, previousAuto)
	t.Cleanup(func() { shared.SetPartySkillRuntime(previous, previousEnabled, previousAuto) })
}

func TestDisablePartyCombatCancelsCastRetriesAndRecoversToStand(t *testing.T) {
	setPartyCombatTestEnabled(t)
	now := time.Now()
	r, _, _ := basicAttackTestRobot(t, now)
	r.partySkillJob = 8
	sender, receiver := comboTestTransport(t, r)
	r.flushPartyDungeonSkillUnsafe(sender, now)
	readComboState(t, receiver, 8, 0, 2)
	shared.SetPartySkillRuntime(nil, false, false)
	r.flushPartyRuntimeUnsafe(sender, now.Add(partyReliableRetryInitial))
	buf := make([]byte, 4096)
	receiver.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 9 || buf[0] != 1 || r.partyTQOSReliableSeq[0][1] != 1 || r.partyBasicAttackStep != 0 {
		t.Fatalf("off switch replayed a cast or reset its sequence: %x", buf[:n])
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 0), 1, r.partyPeers[0])
	r.flushPartyRuntimeUnsafe(sender, now.Add(partySkillRecoverDelay))
	readComboState(t, receiver, 0, 0, 1)
	if !r.partySkillRecoverAt.IsZero() {
		t.Fatal("disabled actor did not finish stand recovery")
	}
	r.buildPartyTQOSRepliesUnsafe(buildPartyTQOSAck(0, 1), 1, r.partyPeers[0])
	r.flushPartyRuntimeUnsafe(sender, now.Add(2*time.Second))
	if r.partyTQOSReliableSeq[0][1] != 2 {
		t.Fatal("disabled actor started another combo")
	}
}
