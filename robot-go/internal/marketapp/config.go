package marketapp

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func DefaultConfig() Config {
	return Config{
		ListenAddr:         "0.0.0.0:8121",
		FridaDB:            "frida",
		AuctionDB:          "taiwan_cain_auction_gold",
		CeraDB:             "taiwan_cain_auction_cera",
		AuctionHost:        "127.0.0.1",
		AuctionPort:        30803,
		CeraHost:           "127.0.0.1",
		CeraPort:           30603,
		ItemInfoSourcePath: "pvf_iteminfo.dat",
		ItemInfoTargets: []string{
			"/home/neople/auction/iteminfo.dat",
			"/home/neople/point/iteminfo.dat",
			"/home/dxf/auction/iteminfo.dat",
			"/home/dxf/point/iteminfo.dat",
		},
		AutoSyncItemInfo: true,
		CeraItemInfoPath: "/home/neople/point/iteminfo.dat",
		SystemOwner: SystemOwner{
			IDBase:      90000001,
			BuyerBase:   90100001,
			NexonBase:   18000000,
			OwnerName:   "market",
			CeraName:    "gold",
			RotateEvery: 10,
		},
		Collector: CollectorCfg{
			Enabled:          true,
			MaxActions:       0,
			MaxConcurrent:    32,
			MaxResultActions: 200,
			PerItemDelayMS:   0,
		},
		Restock: RestockCfg{
			File:             "market_restock.json",
			RandLow:          1,
			RandHigh:         1,
			MaxActions:       0,
			MaxConcurrent:    32,
			MaxResultActions: 200,
			PerItemDelayMS:   0,
		},
		Auto: AutoCfg{
			Enabled:         true,
			Markets:         []string{"auction", "cera"},
			InitialDelayMS:  3000,
			IntervalMS:      30000,
			MaxActions:      0,
			MaxConcurrent:   32,
			ContinueOnError: true,
		},
	}
}

func LoadConfig(configDir string) (Config, string, error) {
	cfg := DefaultConfig()
	path := filepath.Join(configDir, "market_config.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeJSONFile(path, cfg); err != nil {
			return cfg, path, err
		}
		return cfg, path, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, path, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, path, err
	}
	cfg.applyDefaults()
	return cfg, path, nil
}

func (c *Config) applyDefaults() {
	d := DefaultConfig()
	autoUnset := !c.Auto.Enabled && len(c.Auto.Markets) == 0 && c.Auto.InitialDelayMS == 0 && c.Auto.IntervalMS == 0 && c.Auto.MaxActions == 0 && c.Auto.MaxConcurrent == 0 && !c.Auto.ContinueOnError
	collectorUnset := !c.Collector.Enabled && c.Collector.MaxActions == 0 && c.Collector.MaxConcurrent == 0 && c.Collector.MaxResultActions == 0 && c.Collector.PerItemDelayMS == 0 && !c.Collector.IncludeSystemOwners
	itemInfoUnset := !c.AutoSyncItemInfo && c.ItemInfoSourcePath == "" && len(c.ItemInfoTargets) == 0
	if c.ListenAddr == "" {
		c.ListenAddr = d.ListenAddr
	}
	if c.FridaDB == "" {
		c.FridaDB = d.FridaDB
	}
	if c.AuctionDB == "" {
		c.AuctionDB = d.AuctionDB
	}
	if c.CeraDB == "" {
		c.CeraDB = d.CeraDB
	}
	if c.AuctionHost == "" {
		c.AuctionHost = d.AuctionHost
	}
	if c.AuctionPort == 0 {
		c.AuctionPort = d.AuctionPort
	}
	if c.CeraHost == "" {
		c.CeraHost = d.CeraHost
	}
	if c.CeraPort == 0 {
		c.CeraPort = d.CeraPort
	}
	if c.ItemInfoSourcePath == "" {
		c.ItemInfoSourcePath = d.ItemInfoSourcePath
	}
	if len(c.ItemInfoTargets) == 0 {
		c.ItemInfoTargets = d.ItemInfoTargets
	}
	if itemInfoUnset {
		c.AutoSyncItemInfo = d.AutoSyncItemInfo
	}
	if c.CeraItemInfoPath == "" {
		c.CeraItemInfoPath = d.CeraItemInfoPath
	}
	if c.SystemOwner.IDBase == 0 {
		c.SystemOwner.IDBase = d.SystemOwner.IDBase
	}
	if c.SystemOwner.BuyerBase == 0 {
		c.SystemOwner.BuyerBase = d.SystemOwner.BuyerBase
	}
	if c.SystemOwner.NexonBase == 0 {
		c.SystemOwner.NexonBase = d.SystemOwner.NexonBase
	}
	if c.SystemOwner.OwnerName == "" {
		c.SystemOwner.OwnerName = d.SystemOwner.OwnerName
	}
	if c.SystemOwner.CeraName == "" {
		c.SystemOwner.CeraName = d.SystemOwner.CeraName
	}
	if c.SystemOwner.RotateEvery <= 0 {
		c.SystemOwner.RotateEvery = d.SystemOwner.RotateEvery
	}
	if c.Collector.MaxConcurrent <= 0 {
		c.Collector.MaxConcurrent = d.Collector.MaxConcurrent
	}
	if c.Collector.MaxResultActions <= 0 {
		c.Collector.MaxResultActions = d.Collector.MaxResultActions
	}
	if c.Collector.PerItemDelayMS < 0 {
		c.Collector.PerItemDelayMS = d.Collector.PerItemDelayMS
	}
	if collectorUnset {
		c.Collector.Enabled = d.Collector.Enabled
	}
	if c.Restock.File == "" {
		c.Restock.File = d.Restock.File
	}
	if c.Restock.RandLow <= 0 {
		c.Restock.RandLow = d.Restock.RandLow
	}
	if c.Restock.RandHigh <= 0 {
		c.Restock.RandHigh = d.Restock.RandHigh
	}
	if c.Restock.RandHigh < c.Restock.RandLow {
		c.Restock.RandHigh = c.Restock.RandLow
	}
	if c.Restock.MaxConcurrent <= 0 {
		c.Restock.MaxConcurrent = d.Restock.MaxConcurrent
	}
	if c.Restock.MaxResultActions <= 0 {
		c.Restock.MaxResultActions = d.Restock.MaxResultActions
	}
	if c.Restock.PerItemDelayMS < 0 {
		c.Restock.PerItemDelayMS = d.Restock.PerItemDelayMS
	}
	if len(c.Auto.Markets) == 0 {
		c.Auto.Markets = d.Auto.Markets
	}
	if c.Auto.InitialDelayMS < 0 {
		c.Auto.InitialDelayMS = d.Auto.InitialDelayMS
	}
	if c.Auto.IntervalMS <= 0 {
		c.Auto.IntervalMS = d.Auto.IntervalMS
	}
	if c.Auto.MaxConcurrent <= 0 {
		c.Auto.MaxConcurrent = d.Auto.MaxConcurrent
	}
	if autoUnset {
		c.Auto.Enabled = d.Auto.Enabled
		c.Auto.ContinueOnError = d.Auto.ContinueOnError
	}
}

func writeJSONFile(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
