package layout

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBuildsCategorizedRuntimePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config")
	paths := New(root)
	checks := map[string]string{
		paths.MainConfig():      filepath.Join(root, "conf", "config.ini"),
		paths.NameTemplates():   filepath.Join(root, "templates", "robot_name_templates.json"),
		paths.PrivateKey():      filepath.Join(root, "keys", "privatekey.pem"),
		paths.RobotLog():        filepath.Join(root, "logs", "robot.log"),
		paths.StorePointCache(): filepath.Join(root, "state", "store_points_cache.json"),
	}
	for got, want := range checks {
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	}
}

func TestInvalidRootNeverFallsBackToWorkingDirectory(t *testing.T) {
	for _, root := range []string{"", " \t", ".", "config", filepath.Join("srv", "robot", "config")} {
		paths := New(root)
		if paths != (Paths{}) || paths.Valid() {
			t.Fatalf("invalid root %q produced paths: %+v", root, paths)
		}
		for name, path := range map[string]string{
			"main config":       paths.MainConfig(),
			"robot config":      paths.RobotConfig(),
			"market config":     paths.MarketConfig(),
			"mailbox guard":     paths.MailboxGuard(),
			"name templates":    paths.NameTemplates(),
			"shout templates":   paths.ShoutTemplates(),
			"store titles":      paths.StoreTitles(),
			"party skills":      paths.PartySkills(),
			"private key":       paths.PrivateKey(),
			"PVF manifest":      paths.PVFManifest(),
			"PVF iteminfo":      paths.PVFItemInfo(),
			"robot log":         paths.RobotLog(),
			"store point cache": paths.StorePointCache(),
		} {
			if path != "" {
				t.Fatalf("root %q synthesized %s path %q", root, name, path)
			}
		}
		if err := paths.Ensure(); err == nil {
			t.Fatalf("root %q unexpectedly ensured the working directory", root)
		}
		if _, err := paths.AuctionGuardBackup(filepath.Join(t.TempDir(), "df_game_r.js")); err == nil {
			t.Fatalf("root %q unexpectedly produced a relative backup path", root)
		}
	}
}

func TestExternalPatchBackupsStayInStateAndIdentifyTarget(t *testing.T) {
	paths := New(filepath.Join(t.TempDir(), "config"))
	target := filepath.Join(t.TempDir(), "dp2", "df_game_r.js")
	backup, err := paths.AuctionGuardBackup(target)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(paths.State, "backups", "auction_guard")
	relative, err := filepath.Rel(root, backup)
	if err != nil {
		t.Fatal(err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("backup escaped state: %s", backup)
	}
	if filepath.Base(backup) != filepath.Base(target) || !strings.Contains(relative, "dp2") {
		t.Fatalf("backup %q does not identify target %q", backup, target)
	}
}

func TestExternalPatchBackupRejectsEmptyTarget(t *testing.T) {
	if _, err := New(t.TempDir()).PVFUpgradeSeparateBackup(" "); err == nil {
		t.Fatal("empty target accepted")
	}
}
