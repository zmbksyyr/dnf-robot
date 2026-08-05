package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadLauncherConfigUsesDefaultsWhenFileIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), launcherConfigName)
	config, err := loadLauncherConfig(path)
	if err != nil {
		t.Fatalf("loadLauncherConfig: %v", err)
	}
	if config != defaultLauncherConfig() {
		t.Fatalf("config = %+v, want defaults %+v", config, defaultLauncherConfig())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default config was not created: %v", err)
	}
	if string(raw) != string(formatLauncherConfig(defaultLauncherConfig())) {
		t.Fatalf("generated config = %q", raw)
	}
}

func TestLoadLauncherConfigUsesConfiguredSSHValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), launcherConfigName)
	raw := `[ssh]
host = 10.0.0.8
port = 2222
user = deploy
password = secret=value
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := loadLauncherConfig(path)
	if err != nil {
		t.Fatalf("loadLauncherConfig: %v", err)
	}
	want := launcherConfig{Host: "10.0.0.8", Port: "2222", User: "deploy", Password: "secret=value", FreshInstall: true}
	if config != want {
		t.Fatalf("config = %+v, want %+v", config, want)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "[deploy]") || !strings.Contains(string(updated), "fresh_install = 1") || !strings.Contains(string(updated), "0 = 保留配置升级") {
		t.Fatalf("legacy launcher config was not migrated with documented deploy settings: %q", updated)
	}
}

func TestLoadLauncherConfigDisablesFreshInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), launcherConfigName)
	raw := `[ssh]
host = 10.0.0.8

[deploy]
fresh_install = 0
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := loadLauncherConfig(path)
	if err != nil {
		t.Fatalf("loadLauncherConfig: %v", err)
	}
	if config.FreshInstall {
		t.Fatal("fresh_install=0 was not applied")
	}
}

func TestBackupAndResetConfigCommandBacksUpBeforeCreatingEmptyConfig(t *testing.T) {
	command := backupAndResetConfigCommand("/root/config.bak.test")
	for _, want := range []string{
		"ls -1d -- /root/config.bak.*",
		"sort -r",
		"backup_keep=3",
		`if [ "$backup_count" -gt "$backup_keep" ]`,
		`case "$old_backup" in /root/config.bak.*)`,
		"mv -- /root/config /root/config.bak.test",
		"mkdir -m 755 /root/config",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("command %q does not contain %q", command, want)
		}
	}
	moveIndex := strings.Index(command, "mv -- /root/config")
	pruneIndex := strings.Index(command, "rm -rf")
	if moveIndex < 0 || pruneIndex < 0 || moveIndex > pruneIndex {
		t.Fatalf("current config must be moved successfully before old backups are pruned: %q", command)
	}
}

func TestLauncherConfigDocumentsFreshInstallModes(t *testing.T) {
	raw := string(formatLauncherConfig(defaultLauncherConfig()))
	for _, want := range []string{
		"[deploy]",
		"fresh_install = 1",
		"1 = 全新安装",
		"0 = 保留配置升级",
		"重启 robot”始终保留 /root/config",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("launcher config does not contain %q: %q", want, raw)
		}
	}
}

func TestLoadLauncherConfigInvalidFreshInstallPreservesRemoteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), launcherConfigName)
	if err := os.WriteFile(path, []byte("[deploy]\nfresh_install = yes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	config, err := loadLauncherConfig(path)
	if err != nil {
		t.Fatalf("loadLauncherConfig: %v", err)
	}
	if config.FreshInstall {
		t.Fatal("invalid fresh_install enabled destructive deployment")
	}
}

func TestRobotStartCommandUsesLayoutLogDirectory(t *testing.T) {
	for _, want := range []string{
		"/root/robot 2>&1",
		"/root/robot --bounded-log-sink",
		"mkdir -p /root/config/logs",
		"--bounded-log-sink /root/config/logs/stdout.log",
		"2>/root/config/logs/start_error.log",
	} {
		if !strings.Contains(robotStartCommand, want) {
			t.Fatalf("robotStartCommand %q does not contain %q", robotStartCommand, want)
		}
	}
	if strings.Contains(robotStartCommand, "./robot") {
		t.Fatalf("robotStartCommand uses a relative deployment path: %q", robotStartCommand)
	}
}

func TestLoadLauncherConfigKeepsDefaultsForMissingOrEmptyValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), launcherConfigName)
	raw := `[SSH]
host = 10.0.0.9
user =
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := loadLauncherConfig(path)
	if err != nil {
		t.Fatalf("loadLauncherConfig: %v", err)
	}
	want := defaultLauncherConfig()
	want.Host = "10.0.0.9"
	if config != want {
		t.Fatalf("config = %+v, want %+v", config, want)
	}
}

func TestLoadLauncherConfigKeepsDefaultPortWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), launcherConfigName)
	raw := `[ssh]
host = 10.0.0.8
user = deploy
password = secret
`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := loadLauncherConfig(path)
	if err != nil {
		t.Fatalf("loadLauncherConfig: %v", err)
	}
	if config.Port != "22" {
		t.Fatalf("Port = %q, want 22", config.Port)
	}
}

func TestValidSSHPort(t *testing.T) {
	for _, raw := range []string{"", "abc", "0", "-1", "65536"} {
		if _, err := validSSHPort(raw); err == nil {
			t.Fatalf("validSSHPort(%q) returned nil error", raw)
		}
	}
	for _, raw := range []string{"1", "22", " 2222 ", "65535"} {
		if _, err := validSSHPort(raw); err != nil {
			t.Fatalf("validSSHPort(%q): %v", raw, err)
		}
	}
}

func TestSSHAddress(t *testing.T) {
	tests := []struct {
		host string
		port string
		want string
	}{
		{host: "192.168.200.131", port: "22", want: "192.168.200.131:22"},
		{host: "server.example.com", port: "2222", want: "server.example.com:2222"},
		{host: "2001:db8::1", port: "2200", want: "[2001:db8::1]:2200"},
	}
	for _, tt := range tests {
		got, err := sshAddress(tt.host, tt.port)
		if err != nil {
			t.Fatalf("sshAddress(%q, %q): %v", tt.host, tt.port, err)
		}
		if got != tt.want {
			t.Fatalf("sshAddress(%q, %q) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}

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

func TestRemoteConfigPathUsesConfDirectory(t *testing.T) {
	if remoteConfigPath != "/root/config/conf/config.ini" {
		t.Fatalf("remoteConfigPath = %q", remoteConfigPath)
	}
}

func TestParseRobotListenPortsRejectsOutOfRangePorts(t *testing.T) {
	for _, raw := range []string{
		"[Ports]\nRobotAPI = 0\n",
		"[Ports]\nWeb = 70000\n",
	} {
		if _, err := parseRobotListenPorts(raw); err == nil {
			t.Fatalf("parseRobotListenPorts(%q) returned nil error", raw)
		}
	}
}

func TestParseRobotListenPortsRejectsNonNumericPorts(t *testing.T) {
	if _, err := parseRobotListenPorts("[Ports]\nRobotAPI = invalid\n"); err == nil {
		t.Fatal("parseRobotListenPorts returned nil error for a non-numeric port")
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

func TestWaitForRemoteRobotWaitsForConfigBeforeProcessChecks(t *testing.T) {
	configReads := 0
	processReads := 0
	waits := 0
	probes := robotVerificationProbes{
		readPorts: func() (robotListenPorts, error) {
			configReads++
			if configReads < 3 {
				return robotListenPorts{}, errors.New("config not found")
			}
			return robotListenPorts{robotAPI: 18111, web: 18112}, nil
		},
		readMainPID: func() (string, error) {
			processReads++
			return "123\n", nil
		},
		readSinkPID: func() (string, error) {
			return "456\n", nil
		},
		readListeners: func() (string, error) {
			return "0.0.0.0:18111\n[::]:18112\n", nil
		},
		wait: func() { waits++ },
	}

	pid, err := waitForRemoteRobot(probes, 5, nil)
	if err != nil {
		t.Fatalf("waitForRemoteRobot: %v", err)
	}
	if pid != "123" {
		t.Fatalf("pid = %q, want 123", pid)
	}
	if configReads != 3 || waits != 2 {
		t.Fatalf("configReads=%d waits=%d, want 3 and 2", configReads, waits)
	}
	if processReads != 1 {
		t.Fatalf("process checks = %d, want 1 after config became available", processReads)
	}
}

func TestUploadReaderUsesFixedStreamingCommand(t *testing.T) {
	source := strings.NewReader("robot-binary-content")
	called := false
	err := uploadReader(source, func(command string, stdin io.Reader) error {
		called = true
		if robotUploadCommand != "cat > /root/robot.new" {
			t.Fatalf("robotUploadCommand = %q", robotUploadCommand)
		}
		if command != robotUploadCommand {
			t.Fatalf("upload command = %q", command)
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		if string(data) != "robot-binary-content" {
			t.Fatalf("uploaded data = %q", data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("uploadReader: %v", err)
	}
	if !called {
		t.Fatal("upload runner was not called")
	}
}

func TestDeployAndRestartShareOneOperationGuard(t *testing.T) {
	dw := &DeployWindow{}
	if !dw.tryBeginOperation() {
		t.Fatal("first operation was rejected")
	}
	if dw.tryBeginOperation() {
		t.Fatal("concurrent operation was accepted")
	}
	dw.finishOperation()
	if !dw.tryBeginOperation() {
		t.Fatal("operation guard was not released")
	}
	dw.finishOperation()
}

func TestStopRemoteRobotRejectsResidualPIDsAfterSIGKILL(t *testing.T) {
	checks := []string{"123\n456\n", "456\n"}
	runs := []string{}
	err := stopRemoteRobot(remoteRobotStopper{
		run: func(command string) error {
			runs = append(runs, command)
			return nil
		},
		output: func(string) (string, error) {
			result := checks[0]
			checks = checks[1:]
			return result, nil
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "残留 PID: 456") {
		t.Fatalf("error = %v, want residual PID failure", err)
	}
	if len(runs) != 2 || !strings.Contains(runs[1], "kill -9 123 456") {
		t.Fatalf("commands = %#v, want SIGTERM followed by SIGKILL", runs)
	}
}

func TestAwaitRemoteCommandTimesOutAndCancels(t *testing.T) {
	result := make(chan remoteCommandResult)
	cancelled := false
	completed := awaitRemoteCommand(result, 20*time.Millisecond, func() {
		cancelled = true
	})
	if completed.err == nil || !strings.Contains(completed.err.Error(), "超时") {
		t.Fatalf("error = %v, want timeout", completed.err)
	}
	if !cancelled {
		t.Fatal("timeout did not cancel the SSH client")
	}
}
