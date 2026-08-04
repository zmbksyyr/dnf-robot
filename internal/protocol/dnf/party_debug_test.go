package dnf

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"robot/internal/shared"
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

func TestPartyDebugVolumeStaysBoundedAcrossLongRepeatedTraffic(t *testing.T) {
	capture := func(repeats int) shared.PartyDebugStatus {
		if current := globalPartyDebug.active.Load(); current != nil {
			stopPartyDebugSession(current, partyDebugStopUser)
			<-current.done
		}
		StartPartyDebug()
		for uid := uint32(17000000); uid < 17000550; uid++ {
			recordPartyDebugPacket(uid, 0, "RX", "GAME", "PARTY_OPTION_SOURCE", "OBSERVED", "startup", make([]byte, 703))
			recordPartyDebugPacket(uid, 0, "TX", "GAME", "NAT_INFO", "OK", "startup", make([]byte, 37))
			recordPartyDebugPacket(uid, 0, "TX", "GAME", "PARTY_OPTION", "OK", "startup", make([]byte, 93))
		}
		recordPartyDebugPacket(17000140, 17000366, "RX", "GAME", "INVITE", "OBSERVED", "request_id=7", []byte{7, 1})
		recordPartyDebugPacket(17000140, 17000366, "TX", "GAME", "ACCEPT", "OK", "request_id=7", []byte{11, 1})
		recordPartyDebugPacket(17000140, 17000366, "--", "GAME", "SNAPSHOT", "OK", "members=2", nil)
		frame := buildPartyTQOSPacket(7, 0, 0, 3, 1, partyTQOSCodec{key: 0x7e})
		for index := 0; index < repeats; index++ {
			recordPartyDebugTransport(17000140, 17000366, "RX", "UDP", 1, "ACCEPTED", "src=192.168.200.131:5063", frame)
			recordPartyDebugPacket(17000140, 17000366, "--", "CORE", "TRANSPORT_PARSE", "FAIL", "route=1 payload=broken", nil)
		}
		recordPartyDebugPacket(17000140, 17000366, "--", "CORE", "TQOS_READY", "OK", "route=1", nil)
		return StopPartyDebug()
	}

	short := capture(60)
	long := capture(3000)
	for name, result := range map[string]shared.PartyDebugStatus{"short": short, "long": long} {
		joined := strings.Join(result.ReportLines, "\n")
		if result.BytesUsed > 32*1024 {
			t.Fatalf("%s capture grew too large: %d bytes", name, result.BytesUsed)
		}
		if len(result.ReportLines) > 36 || len(joined) > 10*1024 {
			t.Fatalf("%s report is not screenshot bounded: lines=%d bytes=%d\n%s", name, len(result.ReportLines), len(joined), joined)
		}
		if strings.Contains(joined, "17000549") {
			t.Fatalf("%s report retained unrelated startup robots:\n%s", name, joined)
		}
		for _, want := range []string{"RESULT=SUCCESS", "INVITE", "ACCEPT", "SNAPSHOT", "TQOS_READY", "SUPPRESSED="} {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s report missing %q:\n%s", name, want, joined)
			}
		}
		if count := strings.Count(joined, "CHECK FAIL"); count != 1 {
			t.Fatalf("%s repeated failures were not aggregated: count=%d\n%s", name, count, joined)
		}
	}
	if len(short.ReportLines) != len(long.ReportLines) {
		t.Fatalf("duration-equivalent repeated traffic changed report height: short=%d long=%d", len(short.ReportLines), len(long.ReportLines))
	}
}
