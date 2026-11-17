// Package hostprofile describes the small set of differences between Robot
// and the service host it controls. Business code stays independent of host
// process names and paths; a profile is selected once per instance and then
// reused on every restart.
package hostprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"robot/internal/foundation/atomicfile"
	"robot/internal/foundation/config"
	"robot/internal/foundation/lockhub"
)

const currentVersion = 1

type Service struct {
	ProcessName string   `json:"process_name,omitempty"`
	Executable  string   `json:"executable,omitempty"`
	PIDFile     string   `json:"pid_file,omitempty"`
	Image       string   `json:"image,omitempty"`
	ImageSHA256 string   `json:"image_sha256,omitempty"`
	Start       []string `json:"start,omitempty"`
	Directory   string   `json:"directory,omitempty"`
	StopEvent   string   `json:"stop_event,omitempty"`
}

var active struct {
	lockhub.RWLocker
	profile Profile
}

func Configure(p Profile)                    { active.Lock(); active.profile = p; active.Unlock() }
func Current() Profile                       { active.RLock(); defer active.RUnlock(); return active.profile }
func Enabled() bool                          { return Current().Version == currentVersion }
func ServiceFor(name string) (Service, bool) { p := Current(); s, ok := p.Services[name]; return s, ok }

type Profile struct {
	Version      int                `json:"version"`
	Adapter      string             `json:"adapter"`
	DetectedAt   string             `json:"detected_at"`
	ServiceRoot  string             `json:"service_root,omitempty"`
	Services     map[string]Service `json:"services"`
	Capabilities map[string]bool    `json:"capabilities"`
	MarketOwner  string             `json:"market_owner,omitempty"`
	MemoryModel  string             `json:"memory_model,omitempty"`
}

func Default(cfg *config.SysConfig) Profile {
	adapter := "linux"
	if runtime.GOOS == "windows" {
		adapter = "windows"
	}
	root := ""
	if cfg != nil {
		root = strings.TrimSpace(cfg.ServiceRoot)
	}
	return Profile{
		Version: currentVersion, Adapter: adapter, DetectedAt: time.Now().UTC().Format(time.RFC3339),
		ServiceRoot: root,
		Services: map[string]Service{
			"game":    {ProcessName: "df_game_r"},
			"auction": {ProcessName: "df_auction_r"},
			"point":   {ProcessName: "df_point_r"},
		},
		Capabilities: map[string]bool{
			"auction": true, "point": true, "memory_patch": false,
		},
		MarketOwner: "robot",
	}
}

func Validate(p Profile) error {
	if p.Version != currentVersion {
		return fmt.Errorf("unsupported host profile version %d", p.Version)
	}
	if strings.TrimSpace(p.Adapter) == "" {
		return fmt.Errorf("host profile adapter is empty")
	}
	if p.Services == nil {
		return fmt.Errorf("host profile services are missing")
	}
	for name, service := range p.Services {
		if strings.TrimSpace(name) == "" || (strings.TrimSpace(service.ProcessName) == "" && strings.TrimSpace(service.Executable) == "") {
			return fmt.Errorf("host profile service %q has no process identity", name)
		}
	}
	if p.MarketOwner != "" && p.MarketOwner != "robot" && p.MarketOwner != "controller" {
		return fmt.Errorf("host profile market_owner must be robot or controller")
	}
	return nil
}

func LoadOrDetect(path string, cfg *config.SysConfig) (Profile, bool, error) {
	if strings.TrimSpace(path) == "" {
		return Profile{}, false, fmt.Errorf("empty host profile path")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		var profile Profile
		if err := json.Unmarshal(data, &profile); err != nil {
			return Profile{}, false, fmt.Errorf("decode host profile %s: %w", path, err)
		}
		if err := Validate(profile); err != nil {
			return Profile{}, false, fmt.Errorf("validate host profile %s: %w", path, err)
		}
		return profile, false, nil
	}
	if !os.IsNotExist(err) {
		return Profile{}, false, err
	}
	profile := Default(cfg)
	if err := Validate(profile); err != nil {
		return Profile{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return Profile{}, false, err
	}
	data, err = json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return Profile{}, false, err
	}
	if _, err := atomicfile.WriteFileIfMissing(path, append(data, '\n'), 0644); err != nil {
		return Profile{}, false, err
	}
	return profile, true, nil
}
