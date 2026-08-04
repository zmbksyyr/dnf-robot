package dnf

import "testing"

func TestPartyRuntimeConfigurationUsesConfiguredAccountRange(t *testing.T) {
	previous := *partyRuntimeConfigValue.Load()
	t.Cleanup(func() { partyRuntimeConfigValue.Store(&previous) })

	ConfigurePartyRelayPort(17200)
	ConfigurePartyRelayHost("relay.example.com")
	ConfigurePartyRobotAccountRange(18000000, 18000999)
	if currentPartyRelayPort() != 17200 {
		t.Fatalf("relay port = %d", currentPartyRelayPort())
	}
	if currentPartyRelayHost() != "relay.example.com" {
		t.Fatalf("relay host = %q", currentPartyRelayHost())
	}
	if !isPartyRobotAccount(18000000) || !isPartyRobotAccount(18000999) || isPartyRobotAccount(17999999) || isPartyRobotAccount(18001000) {
		t.Fatal("configured account range was not applied")
	}
}

func TestPartyRuntimeConfigurationRejectsInvalidValues(t *testing.T) {
	previous := *partyRuntimeConfigValue.Load()
	t.Cleanup(func() { partyRuntimeConfigValue.Store(&previous) })

	ConfigurePartyRelayPort(-1)
	ConfigurePartyRelayHost("")
	ConfigurePartyRobotAccountRange(0, -1)
	if currentPartyRelayHost() != "127.0.0.1" || currentPartyRelayPort() != defaultPartyRelayPort || !isPartyRobotAccount(defaultPartyRobotAccountFrom) || !isPartyRobotAccount(defaultPartyRobotAccountTo) {
		t.Fatalf("invalid values did not restore defaults: %+v", partyRuntimeConfigValue.Load())
	}
}
