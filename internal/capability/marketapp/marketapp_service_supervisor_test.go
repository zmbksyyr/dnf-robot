package marketapp

import (
	"os"
	"path/filepath"
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
