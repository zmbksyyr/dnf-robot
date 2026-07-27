package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRobotListenPortsUsesConfiguredPorts(t *testing.T) {
	ports, err := parseRobotListenPorts(`
[Ports]
RobotAPI = 18111
Web = 18112

[Web]
WebPassword = test
`)
	if err != nil {
		t.Fatalf("parseRobotListenPorts: %v", err)
	}
	if ports.robotAPI != 18111 || ports.web != 18112 {
		t.Fatalf("ports = %+v, want RobotAPI=18111 Web=18112", ports)
	}
}

func TestParseRobotListenPortsUsesDefaultsForMissingPorts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want robotListenPorts
	}{
		{
			name: "empty config",
			want: robotListenPorts{robotAPI: defaultRobotAPIPort, web: defaultWebPort},
		},
		{
			name: "missing RobotAPI",
			raw:  "[Ports]\nWeb = 18112\n",
			want: robotListenPorts{robotAPI: defaultRobotAPIPort, web: 18112},
		},
		{
			name: "missing Web",
			raw:  "[Ports]\nRobotAPI = 18111\n",
			want: robotListenPorts{robotAPI: 18111, web: defaultWebPort},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports, err := parseRobotListenPorts(tt.raw)
			if err != nil {
				t.Fatalf("parseRobotListenPorts: %v", err)
			}
			if ports != tt.want {
				t.Fatalf("ports = %+v, want %+v", ports, tt.want)
			}
		})
	}
}

func TestLoadRobotListenPortsReportsMissingRemoteConfig(t *testing.T) {
	_, err := loadRobotListenPorts(func() (string, error) {
		return "", errors.New("file not found")
	})
	if err == nil || !strings.Contains(err.Error(), remoteConfigPath) {
		t.Fatalf("error = %v, want missing remote config path", err)
	}
}

func TestParseRobotListenPortsMatchesRuntimeFallbackForInvalidPorts(t *testing.T) {
	ports, err := parseRobotListenPorts(`
[Ports]
RobotAPI = invalid
Web = 70000
`)
	if err != nil {
		t.Fatalf("parseRobotListenPorts: %v", err)
	}
	want := robotListenPorts{robotAPI: defaultRobotAPIPort, web: defaultWebPort}
	if ports != want {
		t.Fatalf("ports = %+v, want %+v", ports, want)
	}
}

func TestParseRobotListenPortsReportsMalformedOversizedLine(t *testing.T) {
	_, err := parseRobotListenPorts("[Ports]\n" + strings.Repeat("x", 70*1024))
	if err == nil {
		t.Fatal("parseRobotListenPorts returned nil error for oversized malformed line")
	}
}

func TestListenerPortsReadyUsesConfiguredPorts(t *testing.T) {
	expected := robotListenPorts{robotAPI: 18111, web: 18112}
	listeners := "0.0.0.0:8111\n0.0.0.0:8112\n0.0.0.0:18111\n[::]:18112\n"
	if !listenerPortsReady(listeners, expected) {
		t.Fatal("configured ports were not detected")
	}
	if listenerPortsReady("0.0.0.0:8111\n0.0.0.0:8112\n", expected) {
		t.Fatal("default ports incorrectly satisfied configured-port verification")
	}
}
