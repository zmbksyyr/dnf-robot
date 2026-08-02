package marketapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	a.stateMu.Lock()
	name := strings.TrimSpace(a.cfg.Restock.CustomPriceFile)
	configDir := a.configDir
	a.stateMu.Unlock()
	if name == "" {
		name = DefaultConfig().Restock.CustomPriceFile
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(configDir, name)
}

func (a *App) refreshCustomPriceRanges() {
	path := a.customPriceRangePath()
	a.stateMu.Lock()
	enabled := a.cfg.Restock.CustomPriceEnabled
	a.stateMu.Unlock()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if writeErr := writeJSONFile(path, defaultCustomPriceRangeDocument()); writeErr != nil {
			a.setPriceRangeState(nil, PriceRangeStatus{Enabled: enabled, Path: path, Error: writeErr.Error()})
			return
		}
	}
	if !enabled {
		a.setPriceRangeState(nil, PriceRangeStatus{Enabled: false, Path: path})
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.setPriceRangeState(nil, PriceRangeStatus{Enabled: true, Path: path, Error: err.Error()})
		return
	}
	var doc customPriceRangeFile
	if err := json.Unmarshal(data, &doc); err != nil {
		a.setPriceRangeState(nil, PriceRangeStatus{Enabled: true, Path: path, Error: err.Error()})
		return
	}
	ranges := make(map[uint32]customPriceRange)
	invalid := 0
	for _, row := range doc.Items {
		if !row.Enabled {
			continue
		}
		if row.ItemID == 0 || row.MinPrice <= 0 || row.MaxPrice < row.MinPrice {
			invalid++
			continue
		}
		ranges[row.ItemID] = row
	}
	status := PriceRangeStatus{Enabled: true, Path: path, LoadedItems: len(ranges), InvalidItems: invalid, LoadedAt: time.Now()}
	if invalid > 0 {
		status.Error = fmt.Sprintf("ignored %d invalid item price ranges", invalid)
	}
	a.setPriceRangeState(ranges, status)
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
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if !a.cfg.Restock.CustomPriceEnabled {
		return customPriceRange{}, false
	}
	rangeCfg, ok := a.priceRanges[itemID]
	return rangeCfg, ok
}
