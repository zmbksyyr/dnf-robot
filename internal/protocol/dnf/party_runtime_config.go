package dnf

import "sync/atomic"

const (
	defaultPartyRelayPort        = 7200
	defaultPartyRobotAccountFrom = 17000000
	defaultPartyRobotAccountTo   = 17000999
)

type partyRuntimeConfig struct {
	relayPort    int
	accountStart uint32
	accountEnd   uint32
}

var partyRuntimeConfigValue atomic.Pointer[partyRuntimeConfig]

func init() {
	partyRuntimeConfigValue.Store(&partyRuntimeConfig{
		relayPort:    defaultPartyRelayPort,
		accountStart: defaultPartyRobotAccountFrom,
		accountEnd:   defaultPartyRobotAccountTo,
	})
}

func ConfigurePartyRelayPort(port int) {
	if port <= 0 || port > 65535 {
		port = defaultPartyRelayPort
	}
	updatePartyRuntimeConfig(func(cfg *partyRuntimeConfig) {
		cfg.relayPort = port
	})
}

func ConfigurePartyRobotAccountRange(start, end int) {
	if start <= 0 || end < start || uint64(end) > uint64(^uint32(0)) {
		start = defaultPartyRobotAccountFrom
		end = defaultPartyRobotAccountTo
	}
	updatePartyRuntimeConfig(func(cfg *partyRuntimeConfig) {
		cfg.accountStart = uint32(start)
		cfg.accountEnd = uint32(end)
	})
}

func updatePartyRuntimeConfig(update func(*partyRuntimeConfig)) {
	for {
		current := partyRuntimeConfigValue.Load()
		next := *current
		update(&next)
		if partyRuntimeConfigValue.CompareAndSwap(current, &next) {
			return
		}
	}
}

func currentPartyRelayPort() int {
	return partyRuntimeConfigValue.Load().relayPort
}

func isPartyRobotAccount(accountID uint32) bool {
	cfg := partyRuntimeConfigValue.Load()
	return accountID >= cfg.accountStart && accountID <= cfg.accountEnd
}
