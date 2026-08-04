package dnf

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"robot/internal/foundation/lockhub"
	"robot/internal/shared"
)

const (
	partyDebugMaxDuration = 5 * time.Minute
	partyDebugLimitBytes  = 8 * 1024 * 1024
	partyDebugDataBudget  = 7 * 1024 * 1024
	partyDebugQueueSize   = 4096
	partyDebugEventCost   = 160
)

type partyDebugEvent struct {
	at        time.Time
	uid       uint32
	peer      uint32
	direction string
	channel   string
	kind      string
	decision  string
	note      string
	route     byte
	raw       []byte
}

type partyDebugSession struct {
	started  time.Time
	stopped  atomic.Int64
	used     atomic.Int64
	count    atomic.Int64
	dropped  atomic.Uint64
	writers  atomic.Int64
	stopping atomic.Bool
	reason   atomic.Int32
	events   chan partyDebugEvent
	stop     chan struct{}
	done     chan struct{}
}

type partyDebugStore struct {
	resultMu lockhub.RWLocker
	active   atomic.Pointer[partyDebugSession]
	last     shared.PartyDebugStatus
}

var globalPartyDebug partyDebugStore

const (
	partyDebugStopUser int32 = iota + 1
	partyDebugStopTime
	partyDebugStopMemory
)

func StartPartyDebug() shared.PartyDebugStatus {
	if current := globalPartyDebug.active.Load(); current != nil && !current.stopping.Load() {
		return partyDebugStatus(current, nil)
	}
	session := &partyDebugSession{
		started: time.Now(),
		events:  make(chan partyDebugEvent, partyDebugQueueSize),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	if !globalPartyDebug.active.CompareAndSwap(nil, session) {
		current := globalPartyDebug.active.Load()
		if current != nil {
			return partyDebugStatus(current, nil)
		}
		globalPartyDebug.active.Store(session)
	}
	globalPartyDebug.resultMu.Lock()
	globalPartyDebug.last = shared.PartyDebugStatus{}
	globalPartyDebug.resultMu.Unlock()
	go collectPartyDebug(session)
	go func() {
		timer := time.NewTimer(partyDebugMaxDuration)
		defer timer.Stop()
		select {
		case <-timer.C:
			stopPartyDebugSession(session, partyDebugStopTime)
		case <-session.done:
		}
	}()
	return partyDebugStatus(session, nil)
}

func StopPartyDebug() shared.PartyDebugStatus {
	session := globalPartyDebug.active.Load()
	if session == nil {
		return PartyDebugStatus()
	}
	stopPartyDebugSession(session, partyDebugStopUser)
	select {
	case <-session.done:
	case <-time.After(5 * time.Second):
	}
	return PartyDebugStatus()
}

func PartyDebugStatus() shared.PartyDebugStatus {
	if session := globalPartyDebug.active.Load(); session != nil {
		return partyDebugStatus(session, nil)
	}
	globalPartyDebug.resultMu.RLock()
	result := globalPartyDebug.last
	result.ReportLines = append([]string(nil), result.ReportLines...)
	globalPartyDebug.resultMu.RUnlock()
	if result.State == "" {
		result.State = "idle"
		result.LimitBytes = partyDebugLimitBytes
	}
	return result
}

func stopPartyDebugSession(session *partyDebugSession, reason int32) {
	if session == nil || !session.stopping.CompareAndSwap(false, true) {
		return
	}
	session.reason.Store(reason)
	session.stopped.Store(time.Now().UnixNano())
	close(session.stop)
}

func collectPartyDebug(session *partyDebugSession) {
	events := make([]partyDebugEvent, 0, 256)
	defer func() {
		status := partyDebugStatus(session, events)
		status.State = "ready"
		status.ReportLines = buildPartyDebugReport(status, events)
		globalPartyDebug.resultMu.Lock()
		globalPartyDebug.last = status
		globalPartyDebug.resultMu.Unlock()
		globalPartyDebug.active.CompareAndSwap(session, nil)
		close(session.done)
	}()
	for {
		select {
		case event := <-session.events:
			events = append(events, event)
			session.count.Add(1)
		case <-session.stop:
			for {
				select {
				case event := <-session.events:
					events = append(events, event)
					session.count.Add(1)
				default:
					if session.writers.Load() == 0 {
						return
					}
					time.Sleep(time.Millisecond)
				}
			}
		}
	}
}

func recordPartyDebug(event partyDebugEvent) {
	session := globalPartyDebug.active.Load()
	if session == nil {
		return
	}
	event.at = time.Now()
	session.writers.Add(1)
	defer session.writers.Add(-1)
	if session.stopping.Load() {
		return
	}
	cost := int64(partyDebugEventCost + len(event.raw) + len(event.note) + len(event.kind) + len(event.decision))
	for {
		used := session.used.Load()
		if used+cost > partyDebugDataBudget {
			stopPartyDebugSession(session, partyDebugStopMemory)
			return
		}
		if session.used.CompareAndSwap(used, used+cost) {
			break
		}
	}
	if len(event.raw) > 0 {
		event.raw = append([]byte(nil), event.raw...)
	}
	select {
	case session.events <- event:
	default:
		session.used.Add(-cost)
		session.dropped.Add(1)
	}
}

func recordPartyDebugPacket(uid, peer uint32, direction, channel, kind, decision, note string, raw []byte) {
	recordPartyDebug(partyDebugEvent{uid: uid, peer: peer, direction: direction, channel: channel, kind: kind, decision: decision, note: note, raw: raw})
}

func recordPartyDebugTransport(uid, peer uint32, direction, channel string, route byte, decision, note string, raw []byte) {
	recordPartyDebug(partyDebugEvent{uid: uid, peer: peer, direction: direction, channel: channel, kind: "TRANSPORT", decision: decision, note: note, route: route, raw: raw})
}

func (r *RobotVo) RecordPartyDebugBaseline() {
	if r == nil || globalPartyDebug.active.Load() == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	peers := 0
	for _, peer := range r.partyPeers {
		if partyPeerIdentityKnown(peer) {
			peers++
		}
	}
	if r.partyPendingPeer == 0 && peers == 0 {
		return
	}
	note := fmt.Sprintf("state=%d pending=%d self_slot=%d self_slot_known=%t self_unique=%d peers=%d relay=%t udp=%t supervisor=%t",
		r.State, r.partyPendingPeer, r.partySelfPeer.slot, r.partySelfPeer.slotKnown, r.partySelfPeer.uniqueID, peers,
		r.partyRelayConn != nil, r.partyUDPConn != nil && r.partyUDPRunning, r.partySupervisorRun)
	recordPartyDebugPacket(r.UID, 0, "--", "CORE", "BASELINE", "OBSERVED", note, nil)
}

func partyDebugStatus(session *partyDebugSession, events []partyDebugEvent) shared.PartyDebugStatus {
	if session == nil {
		return shared.PartyDebugStatus{State: "idle", LimitBytes: partyDebugLimitBytes}
	}
	stoppedAt := time.Time{}
	if ns := session.stopped.Load(); ns > 0 {
		stoppedAt = time.Unix(0, ns)
	}
	now := time.Now()
	state := "capturing"
	if session.stopping.Load() {
		state = "analyzing"
		if !stoppedAt.IsZero() {
			now = stoppedAt
		}
	}
	status := shared.PartyDebugStatus{
		State:      state,
		StartedAt:  session.started.Format(time.RFC3339Nano),
		ElapsedMS:  now.Sub(session.started).Milliseconds(),
		BytesUsed:  session.used.Load(),
		LimitBytes: partyDebugLimitBytes,
		Dropped:    session.dropped.Load(),
		EventCount: int(session.count.Load()),
		StopReason: partyDebugStopReason(session.reason.Load()),
	}
	if !stoppedAt.IsZero() {
		status.StoppedAt = stoppedAt.Format(time.RFC3339Nano)
	}
	return status
}

func partyDebugStopReason(reason int32) string {
	switch reason {
	case partyDebugStopUser:
		return "USER"
	case partyDebugStopTime:
		return "TIME_LIMIT"
	case partyDebugStopMemory:
		return "MEMORY_LIMIT"
	default:
		return ""
	}
}

type partyDebugRobotSummary struct {
	uid      uint32
	invite   bool
	accept   bool
	snapshot bool
	relay    bool
	udpTX    int
	udpRX    int
	stateTX  [4]int
	stateRX  [4]int
	ackTX    int
	ackRX    int
	ready    bool
	failure  string
}

func buildPartyDebugReport(status shared.PartyDebugStatus, events []partyDebugEvent) []string {
	lines := []string{fmt.Sprintf("PARTY DEBUG  DUR=%dms  DATA=%d/%d  EVENTS=%d  DROPPED=%d  STOP=%s",
		status.ElapsedMS, status.BytesUsed, status.LimitBytes, len(events), status.Dropped, valueOr(status.StopReason, "UNKNOWN"))}
	if len(events) == 0 {
		return append(lines, "RESULT=NO_DATA  BREAK=NO_PARTY_EVENT_CAPTURED")
	}
	events = expandPartyDebugTransportEvents(events)
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	robots := map[uint32]*partyDebugRobotSummary{}
	critical := make([]partyDebugEvent, 0, len(events))
	for _, event := range events {
		summary := robots[event.uid]
		if summary == nil {
			summary = &partyDebugRobotSummary{uid: event.uid}
			robots[event.uid] = summary
		}
		switch event.kind {
		case "INVITE":
			summary.invite = event.direction == "RX" || summary.invite
		case "ACCEPT":
			summary.accept = event.direction == "TX" && event.decision == "OK" || summary.accept
		case "SNAPSHOT":
			summary.snapshot = event.decision == "OK" || summary.snapshot
		case "RELAY_CONNECTED":
			summary.relay = true
		case "UDP":
			if event.direction == "TX" {
				summary.udpTX++
			} else if event.direction == "RX" {
				summary.udpRX++
			}
		case "TQOS_READY":
			summary.ready = true
		}
		if event.channel == "UDP" {
			if event.direction == "TX" {
				summary.udpTX++
			} else if event.direction == "RX" {
				summary.udpRX++
			}
		}
		if strings.HasPrefix(event.kind, "TQOS.S") && len(event.kind) == len("TQOS.S0") {
			state := int(event.kind[len(event.kind)-1] - '0')
			if state >= 0 && state <= 3 {
				if event.direction == "TX" {
					summary.stateTX[state]++
				} else if event.direction == "RX" {
					summary.stateRX[state]++
				}
			}
		}
		if event.kind == "TQOS.ACK" {
			if event.direction == "TX" {
				summary.ackTX++
			} else if event.direction == "RX" {
				summary.ackRX++
			}
		}
		if event.decision == "FAIL" || event.decision == "DROP" || event.decision == "TIMEOUT" {
			summary.failure = event.kind + ":" + event.note
		}
		if event.kind != "RELAY_PACKET" || event.decision == "FAIL" || len(event.raw) <= 128 {
			critical = append(critical, event)
		}
	}
	uids := make([]int, 0, len(robots))
	for uid := range robots {
		uids = append(uids, int(uid))
	}
	sort.Ints(uids)
	result, breakAt := partyDebugVerdict(robots, uids)
	lines = append(lines, "RESULT="+result+"  BREAK="+breakAt)
	lines = append(lines, "UID       INV ACC SNAP RELAY UDP_TX UDP_RX S3_TX S3_RX S0_TX S0_RX S1_TX S1_RX S2_TX S2_RX ACK_TX ACK_RX READY FAILURE")
	for _, value := range uids {
		s := robots[uint32(value)]
		lines = append(lines, fmt.Sprintf("%-9d %-3s %-3s %-4s %-5s %-6d %-6d %-5d %-5d %-5d %-5d %-5d %-5d %-5d %-5d %-6d %-6d %-5s %s",
			s.uid, yesNo(s.invite), yesNo(s.accept), yesNo(s.snapshot), yesNo(s.relay), s.udpTX, s.udpRX,
			s.stateTX[3], s.stateRX[3], s.stateTX[0], s.stateRX[0], s.stateTX[1], s.stateRX[1], s.stateTX[2], s.stateRX[2],
			s.ackTX, s.ackRX, yesNo(s.ready), valueOr(s.failure, "-")))
	}
	lines = append(lines, "TIMELINE: MS EVENT UID DIR CH PEER KIND LEN DECISION NOTE RAW")
	lines = append(lines, compactPartyDebugEvents(status, critical)...)
	lines = append(lines, buildPartyDebugChecks(events)...)
	return lines
}

func expandPartyDebugTransportEvents(events []partyDebugEvent) []partyDebugEvent {
	expanded := make([]partyDebugEvent, 0, len(events))
	for _, event := range events {
		if event.kind != "TRANSPORT" {
			if event.kind == "RELAY_PACKET" && event.note == "" {
				event.note = describePartyRelayHeader(event.raw)
			}
			expanded = append(expanded, event)
			continue
		}
		frames, ok := splitPartyTransportFrames(event.raw)
		if !ok {
			event.kind = "TRANSPORT_FRAME"
			event.decision = "FAIL"
			event.note = fmt.Sprintf("route=%d parse=invalid %s", event.route, event.note)
			expanded = append(expanded, event)
			continue
		}
		for _, frame := range frames {
			derived := event
			derived.raw = frame
			derived.kind, derived.note = describePartyTransport(frame, event.route)
			if event.note != "" {
				derived.note += " " + event.note
			}
			expanded = append(expanded, derived)
		}
	}
	return expanded
}

func partyDebugVerdict(robots map[uint32]*partyDebugRobotSummary, uids []int) (string, string) {
	if len(uids) == 0 {
		return "NO_DATA", "NO_ROBOT_EVENT"
	}
	anyInvite, anySnapshot, anyReady := false, false, false
	for _, uid := range uids {
		s := robots[uint32(uid)]
		anyInvite = anyInvite || s.invite
		anySnapshot = anySnapshot || s.snapshot
		anyReady = anyReady || s.ready
	}
	if anyReady {
		return "SUCCESS", "TRANSPORT_READY"
	}
	for _, uid := range uids {
		s := robots[uint32(uid)]
		if s.failure != "" {
			return "FAIL", fmt.Sprintf("R%d:%s", uid, s.failure)
		}
	}
	if anySnapshot {
		for _, uid := range uids {
			s := robots[uint32(uid)]
			if s.stateTX[3] > 0 && s.stateRX[0] == 0 {
				return "FAIL", fmt.Sprintf("R%d:TQOS_S3_TX>S0_MISSING", uid)
			}
			if s.stateRX[1] > 0 && s.stateTX[2] == 0 {
				return "FAIL", fmt.Sprintf("R%d:TQOS_S1_RX>S2_MISSING", uid)
			}
			if s.stateTX[2] > 0 && s.ackRX == 0 {
				return "FAIL", fmt.Sprintf("R%d:TQOS_S2_TX>ACK_MISSING", uid)
			}
		}
		return "PARTY_CONFIRMED", "SNAPSHOT_OK_TRANSPORT_NOT_READY"
	}
	if anyInvite {
		return "FAIL", "INVITE_ACCEPTED>SNAPSHOT_MISSING"
	}
	return "NO_ATTEMPT", "NO_INVITE_CAPTURED"
}

func compactPartyDebugEvents(status shared.PartyDebugStatus, events []partyDebugEvent) []string {
	started, _ := time.Parse(time.RFC3339Nano, status.StartedAt)
	lines := make([]string, 0, len(events))
	lastKey := ""
	repeats := 0
	flushRepeat := func() {
		if repeats > 0 && len(lines) > 0 {
			lines[len(lines)-1] += fmt.Sprintf(" REPEAT=%d", repeats+1)
		}
		repeats = 0
	}
	for index, event := range events {
		key := fmt.Sprintf("%d|%s|%s|%s|%s|%x", event.uid, event.direction, event.channel, event.kind, event.decision, event.raw)
		if key == lastKey {
			repeats++
			continue
		}
		flushRepeat()
		lastKey = key
		raw := "-"
		if len(event.raw) > 0 {
			raw = hex.EncodeToString(event.raw)
			if len(raw) > 192 {
				raw = raw[:192] + "..."
			}
		}
		lines = append(lines, fmt.Sprintf("+%07d E%03d R%d %s %-5s P%d %-16s L=%d %-8s %s RAW=%s",
			event.at.Sub(started).Milliseconds(), index+1, event.uid, valueOr(event.direction, "--"), valueOr(event.channel, "CORE"), event.peer,
			event.kind, len(event.raw), valueOr(event.decision, "OBS"), valueOr(event.note, "-"), raw))
		if len(lines) >= 80 {
			lines = append(lines, fmt.Sprintf("TIMELINE_TRUNCATED=%d", len(events)-index-1))
			break
		}
	}
	flushRepeat()
	return lines
}

func buildPartyDebugChecks(events []partyDebugEvent) []string {
	lines := []string{"CHECKS:"}
	for _, event := range events {
		if event.decision == "FAIL" || event.decision == "DROP" || event.decision == "TIMEOUT" {
			lines = append(lines, fmt.Sprintf("CHECK FAIL UID=%d LAYER=%s EVENT=%s NOTE=%s", event.uid, event.channel, event.kind, valueOr(event.note, "-")))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "CHECK OK NO_EXPLICIT_PARSE_SEND_OR_ROUTE_FAILURE")
	}
	return lines
}

func describePartyTransport(raw []byte, route byte) (kind, note string) {
	frames, ok := splitPartyTransportFrames(raw)
	if !ok || len(frames) != 1 {
		return "UDP", fmt.Sprintf("route=%d frame_parse=invalid", route)
	}
	frame := frames[0]
	if frame[0] == 0 {
		return "TQOS.ACK", fmt.Sprintf("route=%d slot=%d ack_next=%d", route, frame[1], binary.LittleEndian.Uint32(frame[2:6]))
	}
	packet, ok := parsePartyTQOSPacketWithCodec(frame, route, nil)
	if !ok {
		kind := "DATA.UNRELIABLE"
		if frame[0] == 0x01 {
			kind = "DATA.RELIABLE"
		}
		return kind, fmt.Sprintf("route=%d body=%d", route, len(frame)-9)
	}
	return fmt.Sprintf("TQOS.S%d", packet.state), fmt.Sprintf("route=%d seq=%d slot=%d flags=%d codec=%02x/%d", route, packet.sequence, packet.senderSlot, packet.flags, packet.codec.key, packet.codec.rotate)
}

func yesNo(value bool) string {
	if value {
		return "OK"
	}
	return "-"
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.ReplaceAll(value, "\n", " ")
}
