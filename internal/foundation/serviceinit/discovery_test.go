package serviceinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverExternalPortsFollowsRunScriptConfigs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home", "neople")
	runPath := filepath.Join(t.TempDir(), "run")
	services := []struct {
		name string
		port int
	}{
		{"game", 20011},
		{"monitor", 31303},
		{"auction", 31803},
		{"point", 31603},
		{"relay", 17200},
	}
	var run strings.Builder
	for _, service := range services {
		dir := filepath.Join(root, service.name)
		bin := filepath.Join(dir, "df_"+service.name+"_r")
		cfg := filepath.Join(dir, "cfg", service.name+"_custom.cfg")
		writeTestFile(t, bin, "binary\n", 0755)
		writeTestFile(t, cfg, fmt.Sprintf("%s_port = %d\n", service.name, service.port), 0644)
		fmt.Fprintf(&run, "cd %s && ./df_%s_r ./cfg/%s_custom.cfg start df_%s_r &\n", filepath.ToSlash(dir), service.name, service.name, service.name)
	}
	writeTestFile(t, runPath, run.String(), 0644)

	got := DiscoverExternalPorts(runPath, filepath.Join(t.TempDir(), "missing"))
	checks := []struct {
		name string
		got  PortValue
		want int
	}{
		{"game", got.Game, 20011},
		{"monitor", got.Monitor, 31303},
		{"auction", got.Auction, 31803},
		{"point", got.Point, 31603},
		{"relay", got.Relay, 17200},
	}
	for _, check := range checks {
		if check.got.Port != check.want || !strings.HasSuffix(filepath.ToSlash(check.got.Source), check.name+"_custom.cfg") {
			t.Fatalf("%s discovery = %+v, want port %d and config source", check.name, check.got, check.want)
		}
	}
}

func TestPortFromConfigUsesSpecificKeyAndRejectsAmbiguity(t *testing.T) {
	port, err := PortFromConfig("auction", `
db_port = 3306
listen_port = 30000
auction_port = 31803
`)
	if err != nil || port != 31803 {
		t.Fatalf("specific port = %d, %v", port, err)
	}

	if _, err := PortFromConfig("auction", "listen_port = 30803\ntcp_port = 31803\n"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous config error = %v", err)
	}
}

func TestPortFromConfigAcceptsXMLAttributeAndCommandPort(t *testing.T) {
	port, err := PortFromConfig("monitor", `<server listen_port="31303" database_port="3306"/>`)
	if err != nil || port != 31303 {
		t.Fatalf("xml port = %d, %v", port, err)
	}

	port, source, err := PortFromLaunch("relay", Launch{Args: []string{"--port=17200"}, Source: "/root/run"})
	if err != nil || port != 17200 || source != "/root/run command" {
		t.Fatalf("command port = %d source=%q err=%v", port, source, err)
	}
}

func TestDiscoverLaunchRejectsAmbiguousHomeCandidates(t *testing.T) {
	home := t.TempDir()
	for _, vendor := range []string{"one", "two"} {
		dir := filepath.Join(home, vendor, "point")
		writeTestFile(t, filepath.Join(dir, "df_point_r"), "binary\n", 0755)
		writeTestFile(t, filepath.Join(dir, "cfg", "point_custom.cfg"), "point_port=30603\n", 0644)
	}
	_, err := DiscoverLaunch("point", filepath.Join(home, "missing"), filepath.Join(home, "missing-run"), home)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguity error = %v", err)
	}
}

func writeTestFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}
