package dnf

import (
	"bytes"
	"compress/zlib"
	"fmt"
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
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "SNAPSHOT", "OK", "members=2", []byte{11, 2})
	recordPartyDebugPacket(17000003, 17000004, "--", "CORE", "TQOS_READY", "OK", "route=1", nil)
	result := StopPartyDebug()
	if result.State != "ready" || result.EventCount != 4 || len(result.ReportLines) == 0 {
		t.Fatalf("result = %+v", result)
	}
	joined := strings.Join(result.ReportLines, "\n")
	for _, want := range []string{"PARTY DEBUG BUILD=", "RESULT=SUCCESS", "INV", "TQOS_READY", "request_id=7"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("report missing %q:\n%s", want, joined)
		}
	}
}

func TestPartyDebugSeparatesAttemptsMemberChangesAndRecoveredDrops(t *testing.T) {
	if current := globalPartyDebug.active.Load(); current != nil {
		stopPartyDebugSession(current, partyDebugStopUser)
		<-current.done
	}
	StartPartyDebug()
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "INVITE", "OBSERVED", "request_id=1", []byte{7, 1})
	recordPartyDebugPacket(17000003, 0, "TX", "GAME", "ACCEPT", "OK", "request_id=1", []byte{11, 1})
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "SNAPSHOT", "OK", "members=2", []byte{11, 2})
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "SNAPSHOT", "OK", "members=3", []byte{11, 3})
	recordPartyDebugPacket(17000003, 0, "RX", "UDP", "PEER_LOOKUP", "DROP", "sender_slot=1", []byte{1})
	recordPartyDebugPacket(17000003, 17000004, "--", "CORE", "TQOS_READY", "OK", "route=1", nil)
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "PARTY_CLEAR", "OK", "peers=s0/a18000000", []byte{9, 1})
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "INVITE", "OBSERVED", "request_id=2", []byte{7, 2})
	recordPartyDebugPacket(17000003, 0, "TX", "GAME", "ACCEPT", "OK", "request_id=2", []byte{11, 2})
	recordPartyDebugPacket(17000003, 0, "RX", "GAME", "PARTY_CLEAR", "OK", "peers=-", []byte{9, 2})
	result := StopPartyDebug()
	joined := strings.Join(result.ReportLines, "\n")
	for _, want := range []string{
		"RESULT=PARTIAL JOIN=1/2", "A1 R17000003 JOIN=OK", "STAGE=COMPLETE_CLEARED",
		"A2 R17000003 JOIN=-", "STAGE=CLEARED_BEFORE_JOIN", "members=2", "members=3",
		"STATUS=RECOVERED", "PARTY_CLEAR", "ISSUES:",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("report missing %q:\n%s", want, joined)
		}
	}
}

func TestPartyDebugRecordsPartyDeleteHintWithoutClearingState(t *testing.T) {
	if current := globalPartyDebug.active.Load(); current != nil {
		stopPartyDebugSession(current, partyDebugStopUser)
		<-current.done
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(mustPartyHex(t, "0100220002ffffff486b01ffffffffffff00010000005887dd13")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	vo := &RobotVo{State: StateRun, UID: 17000003, Cipher: newPartyTestCipher(t), partyPendingPeer: 7}
	vo.partySelfPeer = partyIPPeer{accID: vo.UID, uniqueID: 8, slot: 1, slotKnown: true}
	vo.partyPeers[0] = partyIPPeer{accID: 18000000, uniqueID: 9, slot: 0, slotKnown: true}
	StartPartyDebug()
	vo.parsePacket(makePartyRecvPacket(9, compressed.Bytes()))
	result := StopPartyDebug()
	joined := strings.Join(result.ReportLines, "\n")
	if !strings.Contains(joined, "PARTY_DELETE_HINT") || !strings.Contains(joined, "self=s1/a17000003/u8") {
		t.Fatalf("party delete hint was not captured:\n%s", joined)
	}
	if !vo.partyActiveUnsafe() || vo.partySelfPeer.uniqueID != 8 || vo.partyPeers[0].uniqueID != 9 {
		t.Fatalf("global party delete hint cleared local party state: self=%+v peers=%+v", vo.partySelfPeer, vo.partyPeers)
	}
}

func TestPartyDebugSlotValueUsesSlotNotPointer(t *testing.T) {
	slot := byte(3)
	if got := partyDebugSlotValue(&slot); got != "3" {
		t.Fatalf("slot value = %q", got)
	}
	if got := partyDebugSlotValue(nil); got != "UNKNOWN" {
		t.Fatalf("nil slot value = %q", got)
	}
}

func TestPartyDebugReportRemainsSinglePageWithManyAttempts(t *testing.T) {
	if current := globalPartyDebug.active.Load(); current != nil {
		stopPartyDebugSession(current, partyDebugStopUser)
		<-current.done
	}
	StartPartyDebug()
	for index := 0; index < 10; index++ {
		uid := uint32(17000100 + index)
		note := fmt.Sprintf("request_id=%d", index+1)
		recordPartyDebugPacket(uid, 0, "RX", "GAME", "INVITE", "OBSERVED", note, []byte{7, byte(index)})
		recordPartyDebugPacket(uid, 0, "TX", "GAME", "ACCEPT", "OK", note, []byte{11, byte(index)})
		if index%2 == 0 {
			recordPartyDebugPacket(uid, 0, "RX", "GAME", "SNAPSHOT", "OK", fmt.Sprintf("members=%d", index+2), []byte{11, byte(index + 2)})
		} else {
			recordPartyDebugPacket(uid, 0, "--", "GAME", "SNAPSHOT_WAIT", "TIMEOUT", "wait=15s", nil)
		}
		recordPartyDebugPacket(uid, 0, "RX", "GAME", "PARTY_CLEAR", "OK", "peers=-", []byte{9, byte(index)})
	}
	result := StopPartyDebug()
	joined := strings.Join(result.ReportLines, "\n")
	if len(result.ReportLines) > 36 || len(joined) > 10*1024 {
		t.Fatalf("many-attempt report exceeded one page: lines=%d bytes=%d\n%s", len(result.ReportLines), len(joined), joined)
	}
	for _, want := range []string{"RESULT=PARTIAL JOIN=5/10", "ATTEMPTS_OMITTED=6", "CLEARED_BEFORE_JOIN", "MEMBERS_OMITTED="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("many-attempt report missing %q:\n%s", want, joined)
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
		if count := strings.Count(joined, "ISSUE ROOT FAIL"); count != 1 {
			t.Fatalf("%s repeated failures were not aggregated: count=%d\n%s", name, count, joined)
		}
	}
	if len(short.ReportLines) != len(long.ReportLines) {
		t.Fatalf("duration-equivalent repeated traffic changed report height: short=%d long=%d", len(short.ReportLines), len(long.ReportLines))
	}
}

func TestPartyDebugReportKeepsCompleteFailedPacketAndSeparatesDistinctFailures(t *testing.T) {
	if current := globalPartyDebug.active.Load(); current != nil {
		stopPartyDebugSession(current, partyDebugStopUser)
		<-current.done
	}
	first := make([]byte, 64)
	second := make([]byte, 64)
	for index := range first {
		first[index] = byte(index)
		second[index] = byte(index)
	}
	second[len(second)-1] = 0xff

	StartPartyDebug()
	recordPartyDebugPacket(17000044, 0, "RX", "GAME", "INVITE", "OBSERVED", "request_id=5", []byte{7, 1})
	recordPartyDebugPacket(17000044, 0, "TX", "GAME", "ACCEPT", "OK", "request_id=5", []byte{11, 1})
	recordPartyDebugPacket(17000044, 0, "RX", "GAME", "SNAPSHOT_PARSE", "FAIL", "invalid first", first)
	recordPartyDebugPacket(17000044, 0, "RX", "GAME", "SNAPSHOT_PARSE", "FAIL", "invalid second", second)
	result := StopPartyDebug()
	joined := strings.Join(result.ReportLines, "\n")
	for _, raw := range [][]byte{first, second} {
		want := fmt.Sprintf("RAW[64]=%x", raw)
		if !strings.Contains(joined, want) {
			t.Fatalf("report did not retain complete failed packet %q:\n%s", want, joined)
		}
	}
	if count := strings.Count(joined, "SNAPSHOT_PARSE"); count < 2 {
		t.Fatalf("distinct failed packets were merged: count=%d\n%s", count, joined)
	}
	if len(result.ReportLines) > 36 || len(joined) > 10*1024 {
		t.Fatalf("complete packet report exceeded one page: lines=%d bytes=%d\n%s", len(result.ReportLines), len(joined), joined)
	}
}
