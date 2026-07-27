package marketapp

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPlanCeraUsesPointBuyNowShape(t *testing.T) {
	app := testApp(t)
	result := &PlanResult{}
	catalog := map[uint32]catalogItem{
		2675345: {ItemID: 2675345, Kind: "stackable"},
	}
	app.planCera([]ceraRow{{
		ItemID: 2675345, Label: "1000w", RestockPrice: 1200, RestockQty: 1, Enabled: true,
	}}, catalog, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(result.Actions))
	}
	action := result.Actions[0]
	if action.StartPrice != -1 || action.InstantPrice != 1200 || action.CountAddInfo != 1 {
		t.Fatalf("unexpected cera action: %#v", action)
	}
}

func TestPlanCeraOnlyDoesNotRequirePVFCatalog(t *testing.T) {
	app := testApp(t)
	app.repository = &clearStockRepository{stock: map[string]map[uint32]int{
		app.cfg.AuctionDB: {},
		app.cfg.CeraDB:    {},
	}}
	app.cfg.Cera.Items = []ceraRow{{
		ItemID: 2675347, Label: "3000w_gold", RestockPrice: 6000, RestockQty: 1, Enabled: true,
	}}

	result, err := app.Plan(RestockRequest{Market: marketNameCera, MaxActions: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.CeraActions != 1 || result.Summary.AuctionActions != 0 {
		t.Fatalf("summary=%#v actions=%#v", result.Summary, result.Actions)
	}
	data, err := os.ReadFile(marketLogPath(app.configDir))
	if err != nil {
		t.Fatal(err)
	}
	logText := string(data)
	for _, want := range []string{"market_decision", "cera=true", "pvf_ready=false", "cera_enabled=1", "cera_actions=1"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("decision log missing %q in %s", want, logText)
		}
	}
}

func TestDefaultCeraRowsEnable3000W(t *testing.T) {
	rows := defaultCeraRows()
	for _, row := range rows {
		if row.ItemID == 2675347 {
			if row.Label != "3000w_gold" || !row.Enabled {
				t.Fatalf("3000w row = %#v, want enabled 3000w_gold", row)
			}
			return
		}
	}
	t.Fatal("3000w cera row not found")
}

func TestConfigDefaultsMigrate3000WEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Cera.Items = []ceraRow{
		{ItemID: 2675347, Label: "3000w_gold", RestockPrice: 6000, RestockQty: 20, Enabled: false},
	}
	cfg.applyDefaults()
	if !cfg.Cera.Items[0].Enabled {
		t.Fatalf("3000w row stayed disabled: %#v", cfg.Cera.Items[0])
	}
}

func TestPlanCeraSkipsRejectedItem(t *testing.T) {
	app := testApp(t)
	app.ceraRejected = map[uint32]string{2675345: "cera_unlanded"}
	result := &PlanResult{}
	app.planCera([]ceraRow{{
		ItemID: 2675345, Label: "1000w", RestockPrice: 1200, RestockQty: 1, Enabled: true,
	}}, nil, map[uint32]int{}, map[uint32]int{}, result)

	if len(result.Actions) != 0 || len(result.Skipped) != 1 {
		t.Fatalf("actions=%#v skipped=%#v", result.Actions, result.Skipped)
	}
	if result.Skipped[0].Reason != "cera_unlanded" {
		t.Fatalf("skip reason = %q", result.Skipped[0].Reason)
	}
}

func TestCeraRejectedReasonExpires(t *testing.T) {
	app := testApp(t)
	app.ceraRejected = map[uint32]string{2675345: "cera_unlanded"}
	app.ceraRejectedAt = map[uint32]time.Time{2675345: time.Now().Add(-ceraRejectedTTL - time.Second)}

	if reason := app.ceraRejectedReason(2675345); reason != "" {
		t.Fatalf("expired reason = %q, want empty", reason)
	}
	if app.ceraRejectedCount() != 0 {
		t.Fatalf("expired cera rejected count should be 0")
	}
}

func TestPlanCeraRoundRobinsItems(t *testing.T) {
	app := testApp(t)
	result := &PlanResult{}
	app.planCera([]ceraRow{
		{ItemID: 1, Label: "a", RestockPrice: 10, RestockQty: 3, Enabled: true},
		{ItemID: 2, Label: "b", RestockPrice: 20, RestockQty: 3, Enabled: true},
	}, nil, map[uint32]int{}, map[uint32]int{}, result)
	if len(result.Actions) != 6 {
		t.Fatalf("actions = %d, want 6", len(result.Actions))
	}
	got := []uint32{result.Actions[0].ItemID, result.Actions[1].ItemID, result.Actions[2].ItemID, result.Actions[3].ItemID}
	want := []uint32{1, 2, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first actions = %v, want %v", got, want)
	}
}
