package dnf

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
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
	partyDebugRawLimit    = 256
	partyDebugRawHead     = 128
	partyDebugRawTail     = 32
	partyDebugNormalRaw   = 32
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
	seen      *atomic.Uint64
}

type partyDebugSession struct {
	started         time.Time
	stopped         atomic.Int64
	used            atomic.Int64
	count           atomic.Int64
	dropped         atomic.Uint64
	suppressed      atomic.Uint64
	writers         atomic.Int64
	stopping        atomic.Bool
	reason          atomic.Int32
	events          chan partyDebugEvent
	stop            chan struct{}
	done            chan struct{}
	samples         sync.Map
	suppressedKinds sync.Map
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
		status.ReportLines = buildPartyDebugReport(status, events, partyDebugSuppressionSummary(session))
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
	limit := partyDebugSampleLimit(event)
	if limit == 0 {
		partyDebugSuppress(session, event.kind)
		return
	}
	candidate := &atomic.Uint64{}
	value, _ := session.samples.LoadOrStore(partyDebugSampleKey(event), candidate)
	counter := value.(*atomic.Uint64)
	event.seen = counter
	if counter.Add(1) > uint64(limit) {
		partyDebugSuppress(session, event.kind)
		return
	}
	cost := int64(partyDebugEventCost + len(event.raw) + len(event.note) + len(event.kind) + len(event.decision))
	for {
		used := session.used.Load()
		if used+cost > partyDebugDataBudget {
			partyDebugSuppress(session, "MEMORY_BUDGET")
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

func partyDebugSampleLimit(event partyDebugEvent) int {
	if event.decision == "FAIL" || event.decision == "DROP" || event.decision == "TIMEOUT" {
		return 3
	}
	switch event.kind {
	case "PARTY_OPTION_SOURCE", "PARTY_OPTION", "NAT_INFO", "PARTY_INFO", "REALTIME":
		return 0
	case "BASELINE":
		return 1
	case "SNAPSHOT", "PARTY_CLEAR":
		return 2
	case "TRANSPORT":
		return 12
	case "RELAY_PACKET":
		return 8
	case "DATA_FRAME":
		return 2
	default:
		return 4
	}
}

func partyDebugSampleKey(event partyDebugEvent) string {
	key := fmt.Sprintf("%d|%d|%s|%s|%s|%s|%d", event.uid, event.peer, event.direction, event.channel, event.kind, event.decision, event.route)
	switch event.kind {
	case "INVITE", "INVITE_PARSE", "ACCEPT", "PARTY_PENDING", "SNAPSHOT", "PARTY_CLEAR":
		key += "|" + event.note
	}
	if partyDebugNeedsDistinctRaw(event) {
		key += "|raw=" + partyDebugRawFingerprint(event.raw)
	}
	return key
}

func partyDebugNeedsDistinctRaw(event partyDebugEvent) bool {
	return event.decision == "FAIL" && len(event.raw) > 0
}

func partyDebugRawFingerprint(raw []byte) string {
	if len(raw) == 0 {
		return "-"
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:6])
}

func partyDebugSuppress(session *partyDebugSession, kind string) {
	if session == nil {
		return
	}
	session.suppressed.Add(1)
	candidate := &atomic.Uint64{}
	value, _ := session.suppressedKinds.LoadOrStore(valueOr(kind, "OTHER"), candidate)
	value.(*atomic.Uint64).Add(1)
}

func partyDebugSuppressionSummary(session *partyDebugSession) string {
	if session == nil {
		return ""
	}
	type item struct {
		kind  string
		count uint64
	}
	items := make([]item, 0, 8)
	session.suppressedKinds.Range(func(key, value interface{}) bool {
		items = append(items, item{kind: key.(string), count: value.(*atomic.Uint64).Load()})
		return true
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].kind < items[j].kind
	})
	parts := make([]string, 0, 5)
	for index, value := range items {
		if index >= 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%d", value.kind, value.count))
	}
	return strings.Join(parts, " ")
}

func partyDebugSlotValue(slot *byte) string {
	if slot == nil {
		return "UNKNOWN"
	}
	return fmt.Sprintf("%d", *slot)
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
		Suppressed: session.suppressed.Load(),
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
	uid       uint32
	invite    bool
	accept    bool
	snapshot  bool
	relay     bool
	udpTX     int
	udpRX     int
	stateTX   [4]int
	stateRX   [4]int
	ackTX     int
	ackRX     int
	ready     bool
	recovered int
	failure   string
}

type partyDebugAttempt struct {
	index    int
	uid      uint32
	invite   *partyDebugEvent
	accept   *partyDebugEvent
	snapshot *partyDebugEvent
	relay    *partyDebugEvent
	ready    *partyDebugEvent
	clear    *partyDebugEvent
	last     time.Time
	failure  string
}

func buildPartyDebugReport(status shared.PartyDebugStatus, events []partyDebugEvent, suppression string) []string {
	lines := []string{fmt.Sprintf("PARTY DEBUG BUILD=%s DUR=%dms DATA=%d/%d KEPT=%d SUPPRESSED=%d DROPPED=%d STOP=%s",
		partyDebugBuildID(), status.ElapsedMS, status.BytesUsed, status.LimitBytes, len(events), status.Suppressed, status.Dropped, valueOr(status.StopReason, "UNKNOWN"))}
	if len(events) == 0 {
		return append(lines, "RESULT=NO_DATA  BREAK=NO_PARTY_EVENT_CAPTURED")
	}
	events = expandPartyDebugTransportEvents(events)
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	relevant := partyDebugRelevantUIDs(events)
	robots := map[uint32]*partyDebugRobotSummary{}
	critical := make([]partyDebugEvent, 0, len(events))
	for index, event := range events {
		if _, ok := relevant[event.uid]; !ok {
			if _, peerOK := relevant[event.peer]; !peerOK {
				continue
			}
		}
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
			if partyDebugEventRecovered(event, events[index+1:]) {
				summary.recovered++
			} else {
				summary.failure = event.kind + ":" + event.note
			}
		}
		critical = append(critical, event)
	}
	attempts := buildPartyDebugAttempts(critical)
	result, breakAt, joined, ready := partyDebugAttemptVerdict(attempts)
	lines = append(lines, fmt.Sprintf("RESULT=%s JOIN=%d/%d READY=%d/%d BREAK=%s", result, joined, len(attempts), ready, len(attempts), breakAt))
	if suppression != "" {
		lines = append(lines, "SUPPRESS "+suppression)
	}
	displayAttempts := partyDebugDisplayAttempts(attempts, 4)
	lines = append(lines, "ATTEMPTS: ID UID JOIN RELAY READY CLEAR I>A A>J J>R STAGE ERROR")
	for _, attempt := range displayAttempts {
		lines = append(lines, partyDebugAttemptLine(attempt))
	}
	if len(attempts) > len(displayAttempts) {
		lines = append(lines, fmt.Sprintf("ATTEMPTS_OMITTED=%d", len(attempts)-len(displayAttempts)))
	}
	lines = append(lines, partyDebugTransportLines(robots, displayAttempts)...)
	lines = append(lines, partyDebugMemberLines(status, critical, 4)...)
	lines = append(lines, "PACKETS: FIRST..LAST UID>PEER DIR CH KIND COUNT LEN DECISION NOTE RAW")
	lines = append(lines, compactPartyDebugEvents(status, critical)...)
	lines = append(lines, buildPartyDebugChecks(status, critical)...)
	return lines
}

func partyDebugBuildID() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "UNKNOWN"
	}
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "UNKNOWN"
	}
	if len(revision) > 8 {
		revision = revision[:8]
	}
	if modified {
		revision += "*"
	}
	return revision
}

func buildPartyDebugAttempts(events []partyDebugEvent) []*partyDebugAttempt {
	attempts := make([]*partyDebugAttempt, 0, 4)
	active := map[uint32]*partyDebugAttempt{}
	for index := range events {
		event := events[index]
		if event.uid == 0 {
			continue
		}
		if event.kind == "INVITE" && event.direction == "RX" {
			copyEvent := event
			attempt := &partyDebugAttempt{index: len(attempts) + 1, uid: event.uid, invite: &copyEvent, last: event.at}
			attempts = append(attempts, attempt)
			active[event.uid] = attempt
		}
		attempt := active[event.uid]
		if attempt == nil {
			continue
		}
		attempt.last = event.at
		switch event.kind {
		case "ACCEPT":
			if event.direction == "TX" && event.decision == "OK" && attempt.accept == nil {
				copyEvent := event
				attempt.accept = &copyEvent
			}
		case "SNAPSHOT":
			if event.decision == "OK" && attempt.snapshot == nil {
				copyEvent := event
				attempt.snapshot = &copyEvent
			}
		case "RELAY_CONNECTED":
			if attempt.relay == nil {
				copyEvent := event
				attempt.relay = &copyEvent
			}
		case "TQOS_READY":
			if attempt.ready == nil {
				copyEvent := event
				attempt.ready = &copyEvent
			}
		case "PARTY_CLEAR", "PARTY_RETURN_ACK":
			copyEvent := event
			attempt.clear = &copyEvent
			delete(active, event.uid)
		}
		if event.decision == "FAIL" || event.decision == "DROP" || event.decision == "TIMEOUT" {
			if !partyDebugEventRecovered(event, events[index+1:]) {
				attempt.failure = event.kind + ":" + event.note
			}
		}
	}
	return attempts
}

func partyDebugAttemptVerdict(attempts []*partyDebugAttempt) (result, breakAt string, joined, ready int) {
	if len(attempts) == 0 {
		return "NO_ATTEMPT", "NO_INVITE_CAPTURED", 0, 0
	}
	for _, attempt := range attempts {
		if attempt.snapshot != nil {
			joined++
		}
		if attempt.ready != nil {
			ready++
		}
	}
	for index := len(attempts) - 1; index >= 0; index-- {
		if attempts[index].snapshot == nil {
			breakAt = fmt.Sprintf("A%d:%s", attempts[index].index, partyDebugAttemptStage(attempts[index]))
			break
		}
	}
	if breakAt == "" && ready < joined {
		breakAt = "TRANSPORT_PARTIAL"
	}
	if breakAt == "" {
		breakAt = "-"
	}
	if joined == len(attempts) {
		return "SUCCESS", breakAt, joined, ready
	}
	if joined > 0 {
		return "PARTIAL", breakAt, joined, ready
	}
	return "FAIL", breakAt, joined, ready
}

func partyDebugDisplayAttempts(attempts []*partyDebugAttempt, limit int) []*partyDebugAttempt {
	if len(attempts) <= limit {
		return append([]*partyDebugAttempt(nil), attempts...)
	}
	selected := append([]*partyDebugAttempt(nil), attempts...)
	sort.SliceStable(selected, func(i, j int) bool {
		leftFailed, rightFailed := selected[i].snapshot == nil, selected[j].snapshot == nil
		if leftFailed != rightFailed {
			return leftFailed
		}
		return selected[i].index > selected[j].index
	})
	selected = selected[:limit]
	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	return selected
}

func partyDebugAttemptLine(attempt *partyDebugAttempt) string {
	return fmt.Sprintf("A%d R%d JOIN=%s RELAY=%s READY=%s CLEAR=%s I>A=%s A>J=%s J>R=%s STAGE=%s ERR=%s",
		attempt.index, attempt.uid, yesNo(attempt.snapshot != nil), yesNo(attempt.relay != nil), yesNo(attempt.ready != nil), yesNo(attempt.clear != nil),
		partyDebugDuration(attempt.invite, attempt.accept), partyDebugDuration(attempt.accept, attempt.snapshot),
		partyDebugDuration(attempt.snapshot, attempt.ready), partyDebugAttemptStage(attempt), partyDebugFailureKind(attempt.failure))
}

func partyDebugFailureKind(failure string) string {
	if failure == "" {
		return "-"
	}
	if index := strings.IndexByte(failure, ':'); index >= 0 {
		return failure[:index]
	}
	return failure
}

func partyDebugDuration(first, second *partyDebugEvent) string {
	if first == nil || second == nil {
		return "-"
	}
	return fmt.Sprintf("%dms", second.at.Sub(first.at).Milliseconds())
}

func partyDebugAttemptStage(attempt *partyDebugAttempt) string {
	if attempt.accept == nil {
		return "ACCEPT_MISSING"
	}
	if attempt.snapshot == nil {
		if attempt.clear != nil {
			return "CLEARED_BEFORE_JOIN"
		}
		return "SNAPSHOT_MISSING"
	}
	if attempt.ready == nil {
		if attempt.relay == nil {
			return "RELAY_MISSING"
		}
		if attempt.clear != nil {
			return "JOINED_CLEARED_NO_READY"
		}
		return "JOINED_TRANSPORT_PENDING"
	}
	if attempt.clear != nil {
		return "COMPLETE_CLEARED"
	}
	return "COMPLETE"
}

func partyDebugTransportLines(robots map[uint32]*partyDebugRobotSummary, attempts []*partyDebugAttempt) []string {
	lines := []string{}
	seen := map[uint32]bool{}
	for _, attempt := range attempts {
		if seen[attempt.uid] {
			continue
		}
		seen[attempt.uid] = true
		summary := robots[attempt.uid]
		if summary == nil {
			continue
		}
		hasTransport := attempt.snapshot != nil || summary.ready || summary.ackTX != 0 || summary.ackRX != 0
		for state := 0; state < len(summary.stateTX); state++ {
			hasTransport = hasTransport || summary.stateTX[state] != 0 || summary.stateRX[state] != 0
		}
		if !hasTransport {
			continue
		}
		if len(lines) == 0 {
			lines = append(lines, "TRANSPORT: UID UDP S3 S0 S1 S2 ACK READY RECOVERED FINAL")
		}
		lines = append(lines, fmt.Sprintf("R%d UDP=%d/%d S3=%d/%d S0=%d/%d S1=%d/%d S2=%d/%d ACK=%d/%d READY=%s REC=%d FINAL=%s",
			attempt.uid, summary.udpTX, summary.udpRX, summary.stateTX[3], summary.stateRX[3], summary.stateTX[0], summary.stateRX[0],
			summary.stateTX[1], summary.stateRX[1], summary.stateTX[2], summary.stateRX[2], summary.ackTX, summary.ackRX,
			yesNo(summary.ready), summary.recovered, partyDebugFailureKind(summary.failure)))
	}
	return lines
}

func partyDebugMemberLines(status shared.PartyDebugStatus, events []partyDebugEvent, limit int) []string {
	started, _ := time.Parse(time.RFC3339Nano, status.StartedAt)
	changes := make([]partyDebugEvent, 0, 8)
	seen := map[string]bool{}
	for _, event := range events {
		if event.kind != "SNAPSHOT" || event.decision != "OK" {
			continue
		}
		key := fmt.Sprintf("%d|%s", event.uid, event.note)
		if seen[key] {
			continue
		}
		seen[key] = true
		changes = append(changes, event)
	}
	if len(changes) == 0 {
		return nil
	}
	selected := changes
	omitted := 0
	if len(changes) > limit {
		selected = append([]partyDebugEvent{changes[0]}, changes[len(changes)-(limit-1):]...)
		omitted = len(changes) - len(selected)
	}
	lines := []string{"MEMBERS:"}
	for _, event := range selected {
		lines = append(lines, fmt.Sprintf("+%d R%d %s", event.at.Sub(started).Milliseconds(), event.uid, compactPartyDebugText(event.note, 190)))
	}
	if omitted > 0 {
		lines = append(lines, fmt.Sprintf("MEMBERS_OMITTED=%d", omitted))
	}
	return lines
}

func partyDebugEventRecovered(event partyDebugEvent, later []partyDebugEvent) bool {
	if event.decision != "DROP" {
		return false
	}
	for _, next := range later {
		if next.uid != event.uid {
			continue
		}
		if next.kind == "PARTY_CLEAR" {
			return false
		}
		if next.kind == "TQOS_READY" || (next.channel == event.channel && next.direction == "RX" && next.decision == "ACCEPTED") {
			return true
		}
	}
	return false
}

func partyDebugRelevantUIDs(events []partyDebugEvent) map[uint32]struct{} {
	relevant := map[uint32]struct{}{}
	for _, event := range events {
		if event.kind == "PARTY_OPTION_SOURCE" || event.kind == "PARTY_OPTION" || event.kind == "NAT_INFO" || event.kind == "PARTY_INFO" || event.kind == "REALTIME" {
			continue
		}
		if event.uid != 0 {
			relevant[event.uid] = struct{}{}
		}
		if event.peer != 0 {
			relevant[event.peer] = struct{}{}
		}
	}
	return relevant
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

func compactPartyDebugEvents(status shared.PartyDebugStatus, events []partyDebugEvent) []string {
	started, _ := time.Parse(time.RFC3339Nano, status.StartedAt)
	type eventGroup struct {
		first time.Time
		last  time.Time
		event partyDebugEvent
		count int
		seen  uint64
	}
	groups := make([]eventGroup, 0, len(events))
	byKey := map[string]int{}
	for _, event := range events {
		key := partyDebugReportGroupKey(event)
		seen := uint64(1)
		if event.seen != nil {
			seen = event.seen.Load()
		}
		if index, ok := byKey[key]; ok {
			groups[index].last = event.at
			groups[index].count++
			if seen > groups[index].seen {
				groups[index].seen = seen
			}
			continue
		}
		byKey[key] = len(groups)
		groups = append(groups, eventGroup{first: event.at, last: event.at, event: event, count: 1, seen: seen})
	}
	const maxGroups = 8
	selected := make([]eventGroup, len(groups))
	copy(selected, groups)
	sort.SliceStable(selected, func(i, j int) bool {
		left, right := partyDebugEventPriority(selected[i].event), partyDebugEventPriority(selected[j].event)
		if left != right {
			return left < right
		}
		return selected[i].first.After(selected[j].first)
	})
	if len(selected) > maxGroups {
		selected = selected[:maxGroups]
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].first.Before(selected[j].first) })
	lines := make([]string, 0, len(selected)+1)
	for _, group := range selected {
		event := group.event
		raw := partyDebugReportRaw(event)
		first := group.first.Sub(started).Milliseconds()
		last := group.last.Sub(started).Milliseconds()
		count := fmt.Sprintf("%d", group.count)
		if group.seen > uint64(group.count) {
			count += "+"
		}
		lines = append(lines, fmt.Sprintf("+%d..%d R%d>P%d %s %s %-16s N=%s L=%d %s %s %s",
			first, last, event.uid, event.peer, valueOr(event.direction, "--"), valueOr(event.channel, "CORE"),
			event.kind, count, len(event.raw), valueOr(event.decision, "OBS"), compactPartyDebugText(valueOr(event.note, "-"), 160), raw))
	}
	if len(groups) > len(selected) {
		lines = append(lines, fmt.Sprintf("FLOW_OMITTED_GROUPS=%d", len(groups)-len(selected)))
	}
	return lines
}

func partyDebugReportRaw(event partyDebugEvent) string {
	if len(event.raw) == 0 {
		return "RAW=-"
	}
	limit := partyDebugNormalRaw
	if event.decision == "FAIL" || event.decision == "TIMEOUT" {
		limit = partyDebugRawLimit
	}
	if len(event.raw) <= limit {
		return fmt.Sprintf("RAW[%d]=%s", len(event.raw), hex.EncodeToString(event.raw))
	}
	if limit == partyDebugNormalRaw {
		return fmt.Sprintf("RAW[%d]=%s...(+%dB)", len(event.raw), hex.EncodeToString(event.raw[:limit]), len(event.raw)-limit)
	}
	head := partyDebugRawHead
	tail := partyDebugRawTail
	if head+tail > len(event.raw) {
		head = len(event.raw)
		tail = 0
	}
	raw := hex.EncodeToString(event.raw[:head])
	if tail > 0 {
		raw += "..." + hex.EncodeToString(event.raw[len(event.raw)-tail:])
	}
	return fmt.Sprintf("RAW[%d]=%s(+%dB)", len(event.raw), raw, len(event.raw)-head-tail)
}

func partyDebugReportGroupKey(event partyDebugEvent) string {
	key := fmt.Sprintf("%d|%d|%s|%s|%s|%s|%d", event.uid, event.peer, event.direction, event.channel, event.kind, event.decision, event.route)
	if event.kind == "SNAPSHOT" || event.kind == "PARTY_CLEAR" {
		key += "|" + event.note
	}
	if partyDebugNeedsDistinctRaw(event) {
		key += "|raw=" + partyDebugRawFingerprint(event.raw)
	}
	return key
}

func partyDebugEventPriority(event partyDebugEvent) int {
	if event.decision == "FAIL" || event.decision == "DROP" || event.decision == "TIMEOUT" {
		return 0
	}
	switch event.kind {
	case "INVITE", "INVITE_PARSE", "ACCEPT", "ACCEPT_FALLBACK", "PARTY_PENDING", "SNAPSHOT", "SNAPSHOT_PARSE", "SNAPSHOT_WAIT", "PARTY_CLEAR", "PARTY_DELETE_HINT", "ORPHAN_DETECTED", "PARTY_RETURN_ACK", "PARTY_RESELECT", "PARTY_NORMAL":
		return 1
	case "RELAY_CONNECT", "RELAY_AUTH", "RELAY_CONNECTED", "TQOS_READY", "ROUTE_DEGRADED":
		return 2
	case "TQOS.S0", "TQOS.S1", "TQOS.S2", "TQOS.S3", "TQOS.ACK":
		return 3
	case "DATA.RELIABLE", "DATA.UNRELIABLE", "DATA_FRAME", "RELAY_PACKET":
		return 5
	default:
		return 4
	}
}

func buildPartyDebugChecks(status shared.PartyDebugStatus, events []partyDebugEvent) []string {
	started, _ := time.Parse(time.RFC3339Nano, status.StartedAt)
	type checkGroup struct {
		first     time.Time
		last      time.Time
		event     partyDebugEvent
		count     uint64
		recovered bool
	}
	groups := make([]checkGroup, 0, 8)
	byKey := map[string]int{}
	for eventIndex, event := range events {
		if event.decision != "FAIL" && event.decision != "DROP" && event.decision != "TIMEOUT" {
			continue
		}
		key := partyDebugSampleKey(event)
		seen := uint64(1)
		if event.seen != nil {
			seen = event.seen.Load()
		}
		recovered := partyDebugEventRecovered(event, events[eventIndex+1:])
		if index, ok := byKey[key]; ok {
			groups[index].last = event.at
			if seen > groups[index].count {
				groups[index].count = seen
			}
			groups[index].recovered = recovered
			continue
		}
		byKey[key] = len(groups)
		groups = append(groups, checkGroup{first: event.at, last: event.at, event: event, count: seen, recovered: recovered})
	}
	lines := []string{"ISSUES:"}
	const maxChecks = 3
	for index, group := range groups {
		if index >= maxChecks {
			lines = append(lines, fmt.Sprintf("ISSUES_OMITTED=%d", len(groups)-maxChecks))
			break
		}
		statusText := "FINAL"
		if group.recovered {
			statusText = "RECOVERED"
		}
		role := "FOLLOWUP"
		if index == 0 {
			role = "ROOT"
		}
		lines = append(lines, fmt.Sprintf("ISSUE %s %s R%d>P%d %s/%s N=%d +%d..%d STATUS=%s NOTE=%s", role, group.event.decision,
			group.event.uid, group.event.peer, group.event.channel, group.event.kind, group.count,
			group.first.Sub(started).Milliseconds(), group.last.Sub(started).Milliseconds(), statusText, compactPartyDebugText(valueOr(group.event.note, "-"), 140)))
	}
	if len(groups) == 0 {
		lines = append(lines, "ISSUE OK NO_EXPLICIT_PARSE_SEND_OR_ROUTE_FAILURE")
	}
	return lines
}

func compactPartyDebugText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		return value[:limit] + "..."
	}
	return value
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
