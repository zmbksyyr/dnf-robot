package marketapp

import (
	"fmt"
	"os"
	"time"

	foundationconfig "robot/internal/foundation/config"
	"robot/internal/foundation/layout"
)

type customPriceRangeDocument struct {
	Description string             `json:"description"`
	FieldNotes  map[string]string  `json:"field_notes"`
	Version     int                `json:"version"`
	Items       []customPriceRange `json:"items"`
}

func defaultCustomPriceRangeDocument() customPriceRangeDocument {
	return customPriceRangeDocument{
		Description: "拍卖行物品独立最终价格范围。有效且启用的单品配置优先于 market_config.ini 通用公式，并同时用于系统补货与虚拟买家回收判断。",
		FieldNotes: map[string]string{
			"item_id":   "用于匹配的 DNF 物品 ID。",
			"name":      "可选备注名称，仅供管理员识别，不参与匹配。",
			"min_price": "最终最低单价；堆叠物品按单件计算，装备按单条拍卖记录计算。",
			"max_price": "最终最高单价，必须大于或等于 min_price。",
			"enabled":   "是否启用该物品的独立价格覆盖。",
		},
		Version: 1,
		Items:   []customPriceRange{},
	}
}

func (a *App) customPriceRangePath() string {
	return layout.New(a.configDir).MarketPrices()
}

func (a *App) refreshCustomPriceRanges() {
	if a.runtimeFilesWatched.Load() {
		return
	}
	path := a.customPriceRangePath()
	enabled := a.configSnapshot().Restock.CustomPriceEnabled

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if writeErr := writeJSONFile(path, defaultCustomPriceRangeDocument()); writeErr != nil {
			a.setPriceRangeState(nil, PriceRangeStatus{Enabled: enabled, Path: path, Error: writeErr.Error()})
			return
		}
	}
	ranges, status, err := readCustomPriceRanges(path, enabled)
	if err != nil {
		a.setPriceRangeState(nil, PriceRangeStatus{Enabled: enabled, Path: path, Error: err.Error()})
		return
	}
	a.setPriceRangeState(ranges, status)
}

func (a *App) reloadCustomPriceRangeFile(path string) error {
	expected := a.customPriceRangePath()
	if path != expected {
		return nil
	}
	enabled := a.configSnapshot().Restock.CustomPriceEnabled
	ranges, status, err := readCustomPriceRanges(path, enabled)
	if err != nil {
		return err
	}
	a.setPriceRangeState(ranges, status)
	a.appendLog(LogEvent{Type: "config", Status: marketLogStatusSuccess, Message: fmt.Sprintf("market price ranges reloaded: items=%d", status.LoadedItems)})
	return nil
}

func readCustomPriceRanges(path string, enabled bool) (map[uint32]customPriceRange, PriceRangeStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, PriceRangeStatus{}, err
	}
	var doc struct {
		Description string              `json:"description"`
		FieldNotes  map[string]string   `json:"field_notes"`
		Version     int                 `json:"version"`
		Items       *[]customPriceRange `json:"items"`
	}
	if err := foundationconfig.DecodeJSONBytes(data, &doc); err != nil {
		return nil, PriceRangeStatus{}, err
	}
	if doc.Version != 1 || doc.Items == nil {
		return nil, PriceRangeStatus{}, fmt.Errorf("unsupported or incomplete price range document")
	}
	ranges := make(map[uint32]customPriceRange, len(*doc.Items))
	seen := make(map[uint32]struct{}, len(*doc.Items))
	for index, row := range *doc.Items {
		if row.ItemID == 0 || row.MinPrice <= 0 || row.MaxPrice < row.MinPrice {
			return nil, PriceRangeStatus{}, fmt.Errorf("invalid item price range at index %d", index)
		}
		if _, exists := seen[row.ItemID]; exists {
			return nil, PriceRangeStatus{}, fmt.Errorf("duplicate item price range for item_id %d", row.ItemID)
		}
		seen[row.ItemID] = struct{}{}
		if row.Enabled {
			ranges[row.ItemID] = row
		}
	}
	if !enabled {
		return map[uint32]customPriceRange{}, PriceRangeStatus{Enabled: false, Path: path, LoadedAt: time.Now()}, nil
	}
	status := PriceRangeStatus{Enabled: true, Path: path, LoadedItems: len(ranges), LoadedAt: time.Now()}
	return ranges, status, nil
}

func (a *App) setPriceRangeState(ranges map[uint32]customPriceRange, status PriceRangeStatus) {
	if ranges == nil {
		ranges = map[uint32]customPriceRange{}
	}
	a.stateMu.Lock()
	a.priceRanges = ranges
	a.priceRangeStatus = status
	a.stateMu.Unlock()
}

func (a *App) customPriceRange(itemID uint32) (customPriceRange, bool) {
	if !a.configSnapshot().Restock.CustomPriceEnabled {
		return customPriceRange{}, false
	}
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	rangeCfg, ok := a.priceRanges[itemID]
	return rangeCfg, ok
}
