package dnf

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestPartyDebugCapturesAndBuildsDenseReport(t *testing.T) {
	if current := globalPartyDebug.active.Load(); current != nil {
		stopPartyDebugSession(current, partyDebugStopUser)
		<-current.done
	}
	started := StartPartyDebug()
	if started.State != "capturing" || started.LimitBytes != partyDebugLimitBytes {
		t.Fatalf("start status = %+v", started)
	}
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "INVITE", "OBSERVED", "request_id=7", []byte{7, 1})
	recordPartyDebugPacket(17000003, 0, "TX", "GAME", "ACCEPT", "OK", "request_id=7", []byte{11, 1})
	recordPartyDebugPacket(17000003, 17000004, "--", "CORE", "TQOS_READY", "OK", "route=1", nil)
	result := StopPartyDebug()
	if result.State != "ready" || result.EventCount != 3 || len(result.ReportLines) == 0 {
		t.Fatalf("result = %+v", result)
	}
	joined := strings.Join(result.ReportLines, "\n")
	for _, want := range []string{"RESULT=SUCCESS", "INV", "TQOS_READY", "request_id=7"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("report missing %q:\n%s", want, joined)
		}
	}
}

func TestPartyDebugDoesNotChangeTQOSReplies(t *testing.T) {
	newRobot := func() *RobotVo {
		vo := &RobotVo{UID: 17000003}
		vo.partySelfPeer = partyIPPeer{accID: 17000003, slot: 1, slotKnown: true}
		vo.partyPeers[0] = partyIPPeer{accID: 17000004, uniqueID: 7, slot: 0, slotKnown: true, outerIP: net.IPv4(192, 168, 200, 1), port: 5063}
		return vo
	}
	remote := &net.UDPAddr{IP: net.IPv4(192, 168, 200, 1), Port: 5063}
	request := buildPartyTQOSPacket(7, 0, 0, 3, 1, partyTQOSCodec{key: 0x7e})
	without := newRobot().buildPartyUDPAcks(request, remote)

	StartPartyDebug()
	with := newRobot().buildPartyUDPAcks(request, remote)
	StopPartyDebug()
	if len(without) != len(with) {
		t.Fatalf("reply count changed without=%d with=%d", len(without), len(with))
	}
	for index := range without {
		if !bytes.Equal(without[index], with[index]) {
			t.Fatalf("reply %d changed without=%x with=%x", index, without[index], with[index])
		}
	}
}

func TestPartyDebugDisabledDoesNotRetainEvents(t *testing.T) {
	if current := globalPartyDebug.active.Load(); current != nil {
		stopPartyDebugSession(current, partyDebugStopUser)
		<-current.done
	}
	recordPartyDebugPacket(1, 2, "RX", "UDP", "TQOS.S3", "OBSERVED", "disabled", []byte{1})
	status := PartyDebugStatus()
	if status.State == "capturing" || status.State == "analyzing" {
		t.Fatalf("unexpected active recorder: %+v", status)
	}
}
