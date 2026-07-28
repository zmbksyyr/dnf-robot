package marketapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketServiceSpecsFollowDFGameRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home", "dxf")
	auctionItemInfo := filepath.Join(root, "auction", "iteminfo.dat")
	pointItemInfo := filepath.Join(root, "point", "iteminfo.dat")
	app := &App{
		dfGameR: filepath.Join(root, "game", "df_game_r"),
		cfg: Config{
			ItemInfoTargets: []string{
				filepath.Join(t.TempDir(), "home", "neople", "auction", "iteminfo.dat"),
				auctionItemInfo,
				pointItemInfo,
			},
		},
	}

	specs := app.marketServiceSpecs()
	if len(specs) != 2 {
		t.Fatalf("service specs=%d, want 2", len(specs))
	}
	if specs[0].dir != filepath.Join(root, "auction") || specs[1].dir != filepath.Join(root, "point") {
		t.Fatalf("service dirs=%q,%q want root %q", specs[0].dir, specs[1].dir, root)
	}
	if got := app.itemInfoTargetForService(marketServiceNameAuction); got != auctionItemInfo {
		t.Fatalf("auction iteminfo=%q want %q", got, auctionItemInfo)
	}
	if got := app.itemInfoTargetForService(marketServiceNamePoint); got != pointItemInfo {
		t.Fatalf("point iteminfo=%q want %q", got, pointItemInfo)
	}
}

func TestValidateMarketServiceItemInfo(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.dat")
	if err := validateMarketServiceItemInfo(missing); err == nil {
		t.Fatal("missing iteminfo should fail validation")
	}
	empty := filepath.Join(dir, "empty.dat")
	if err := os.WriteFile(empty, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateMarketServiceItemInfo(empty); err == nil {
		t.Fatal("empty iteminfo should fail validation")
	}
	valid := filepath.Join(dir, "iteminfo.dat")
	if err := os.WriteFile(valid, []byte("1 row\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateMarketServiceItemInfo(valid); err != nil {
		t.Fatalf("valid iteminfo failed validation: %v", err)
	}
}

func TestMarketServiceLaunchUsesRunScriptArguments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home", "neople")
	auctionDir := filepath.Join(root, "auction")
	mustWriteServiceFile(t, filepath.Join(auctionDir, "df_auction_r"))
	mustWriteServiceFile(t, filepath.Join(auctionDir, "cfg", "auction_siroco.cfg"))
	runScript := filepath.Join(t.TempDir(), "run")
	if err := os.WriteFile(runScript, []byte("cd "+filepath.ToSlash(auctionDir)+"\n./df_auction_r ./cfg/auction_siroco.cfg start ./df_auction_r &\n"), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{serviceRunScript: runScript, serviceHomeRoot: filepath.Join(t.TempDir(), "missing")}
	launch, err := app.discoverMarketServiceLaunch(marketServiceNameAuction, root)
	if err != nil {
		t.Fatal(err)
	}
	if launch.dir != auctionDir || launch.bin != "./df_auction_r" {
		t.Fatalf("launch=%+v", launch)
	}
	if got := strings.Join(launch.args, " "); got != "./cfg/auction_siroco.cfg start ./df_auction_r" {
		t.Fatalf("args=%q", got)
	}
	if launch.source != runScript {
		t.Fatalf("source=%q want %q", launch.source, runScript)
	}
}

func TestMarketServiceLaunchFallsBackToPreferredServiceDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home", "dxf")
	pointDir := filepath.Join(root, "point")
	mustWriteServiceFile(t, filepath.Join(pointDir, "df_point_r"))
	mustWriteServiceFile(t, filepath.Join(pointDir, "cfg", "point_custom.cfg"))
	app := &App{
		serviceRunScript: filepath.Join(t.TempDir(), "missing-run"),
		serviceHomeRoot:  filepath.Join(t.TempDir(), "missing-home"),
	}

	launch, err := app.discoverMarketServiceLaunch(marketServiceNamePoint, root)
	if err != nil {
		t.Fatal(err)
	}
	if launch.dir != pointDir || launch.source != "service root" {
		t.Fatalf("launch=%+v", launch)
	}
	if got := strings.Join(launch.args, " "); got != "./cfg/point_custom.cfg start df_point_r" {
		t.Fatalf("args=%q", got)
	}
}

func TestMarketServiceLaunchFallsBackToHomeScan(t *testing.T) {
	temp := t.TempDir()
	homeRoot := filepath.Join(temp, "home")
	auctionDir := filepath.Join(homeRoot, "vendor", "release", "auction")
	mustWriteServiceFile(t, filepath.Join(auctionDir, "df_auction_r"))
	mustWriteServiceFile(t, filepath.Join(auctionDir, "cfg", "auction_vendor.cfg"))
	app := &App{
		serviceRunScript: filepath.Join(temp, "missing-run"),
		serviceHomeRoot:  homeRoot,
	}

	launch, err := app.discoverMarketServiceLaunch(marketServiceNameAuction, filepath.Join(homeRoot, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if launch.dir != auctionDir || launch.source != "home scan" {
		t.Fatalf("launch=%+v", launch)
	}
}

func TestMarketServiceLaunchRejectsAmbiguousHomeCandidates(t *testing.T) {
	temp := t.TempDir()
	homeRoot := filepath.Join(temp, "home")
	for _, vendor := range []string{"one", "two"} {
		dir := filepath.Join(homeRoot, vendor, "point")
		mustWriteServiceFile(t, filepath.Join(dir, "df_point_r"))
		mustWriteServiceFile(t, filepath.Join(dir, "cfg", "point_"+vendor+".cfg"))
	}
	app := &App{
		serviceRunScript: filepath.Join(temp, "missing-run"),
		serviceHomeRoot:  homeRoot,
	}

	_, err := app.discoverMarketServiceLaunch(marketServiceNamePoint, filepath.Join(homeRoot, "missing"))
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
}

func mustWriteServiceFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test\n"), 0755); err != nil {
		t.Fatal(err)
	}
}
