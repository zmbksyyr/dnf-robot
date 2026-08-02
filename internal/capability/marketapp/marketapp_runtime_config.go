package marketapp

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	foundationconfig "robot/internal/foundation/config"
)

var marketConfigKeys = map[string]map[string]bool{
	"database": {
		"game_db": true, "auction_db": true, "cera_db": true,
	},
	"iteminfo": {
		"targets": true, "source_path": true,
	},
	"system_owner": {
		"id_base": true, "buyer_base": true, "owner_name": true, "cera_name": true, "rotate_every": true,
	},
	"auction_price": {
		"quality_filter": true, "stack_sizes": true,
		"equipment_qty_min": true, "equipment_qty_max": true,
		"equipment_level_min": true, "equipment_level_max": true,
		"equip_inflate_min": true, "equip_inflate_max": true,
		"upgrade_min": true, "upgrade_max": true, "upgrade_price_rate": true,
		"rand_low": true, "rand_high": true, "custom_price_enabled": true, "custom_price_file": true,
		"max_actions": true, "max_concurrent": true, "max_result_actions": true, "per_item_delay_ms": true,
	},
	"auction_collect": {
		"enabled": true, "include_system_owners": true, "price_range_enabled": true,
		"in_range_probability": true, "out_of_range_probability": true,
		"max_actions": true, "max_concurrent": true,
	},
	"cera": {
		"items": true,
	},
	"auto": {
		"enabled": true, "markets": true, "initial_delay_ms": true, "interval_ms": true,
		"max_actions": true, "max_concurrent": true, "continue_on_error": true,
	},
}

func loadConfigSnapshot(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := validateMarketINI(data); err != nil {
		return Config{}, err
	}
	ini, err := foundationconfig.LoadFromString(string(data))
	if err != nil {
		return Config{}, err
	}
	cfg, err := decodeMarketINI(ini)
	if err != nil {
		return Config{}, err
	}
	if err := validateMarketConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateMarketINI(data []byte) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	section := ""
	settings := 0
	seenSettings := make(map[string]int)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || len(line) < 3 {
				return fmt.Errorf("market config line %d has an invalid section", lineNumber)
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section == "" {
				return fmt.Errorf("market config line %d has an empty section", lineNumber)
			}
			if section != strings.ToLower(section) {
				return fmt.Errorf("market config line %d section %q must use canonical lowercase", lineNumber, section)
			}
			if _, ok := marketConfigKeys[section]; !ok {
				return fmt.Errorf("market config line %d has unknown section %q", lineNumber, section)
			}
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator <= 0 || section == "" {
			return fmt.Errorf("market config line %d is outside a valid section", lineNumber)
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if key == "" {
			return fmt.Errorf("market config line %d has an empty key", lineNumber)
		}
		if key != strings.ToLower(key) {
			return fmt.Errorf("market config line %d key %q must use canonical lowercase", lineNumber, key)
		}
		if !marketConfigKeys[section][key] {
			return fmt.Errorf("market config line %d has unknown setting %s.%s", lineNumber, section, key)
		}
		name := section + "." + key
		if firstLine := seenSettings[name]; firstLine != 0 {
			return fmt.Errorf("market config line %d duplicates %s first defined at line %d", lineNumber, name, firstLine)
		}
		seenSettings[name] = lineNumber
		if err := validateMarketValue(section, key, value); err != nil {
			return fmt.Errorf("market config line %d %s.%s: %w", lineNumber, section, key, err)
		}
		settings++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if settings == 0 {
		return fmt.Errorf("market config contains no settings")
	}
	return nil
}

func validateMarketValue(section, key, value string) error {
	name := section + "." + key
	switch name {
	case "iteminfo.source_path":
		return fmt.Errorf("is not supported; the source is fixed at config/pvf/iteminfo.dat")
	case "auction_price.custom_price_file":
		return fmt.Errorf("is not supported; prices are fixed at config/conf/market_item_price_ranges.json")
	case "system_owner.id_base", "system_owner.buyer_base":
		if _, err := strconv.ParseUint(value, 10, 32); err != nil {
			return fmt.Errorf("must be an unsigned integer")
		}
	case "system_owner.rotate_every",
		"auction_price.equipment_qty_min", "auction_price.equipment_qty_max",
		"auction_price.equipment_level_min", "auction_price.equipment_level_max",
		"auction_price.equip_inflate_min", "auction_price.equip_inflate_max",
		"auction_price.upgrade_min", "auction_price.upgrade_max",
		"auction_price.max_actions", "auction_price.max_concurrent",
		"auction_price.max_result_actions", "auction_price.per_item_delay_ms",
		"auction_collect.max_actions", "auction_collect.max_concurrent",
		"auto.initial_delay_ms", "auto.interval_ms", "auto.max_actions", "auto.max_concurrent":
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("must be an integer")
		}
	case "auction_price.upgrade_price_rate", "auction_price.rand_low", "auction_price.rand_high",
		"auction_collect.in_range_probability", "auction_collect.out_of_range_probability":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("must be a number")
		}
	case "auction_price.quality_filter", "auction_price.custom_price_enabled",
		"auction_collect.enabled", "auction_collect.include_system_owners", "auction_collect.price_range_enabled",
		"auto.enabled", "auto.continue_on_error":
		if value != "true" && value != "false" {
			return fmt.Errorf("must be true or false")
		}
	case "auction_price.stack_sizes":
		values := splitStrings(value)
		if len(values) == 0 {
			return fmt.Errorf("must contain at least one positive integer")
		}
		for _, item := range values {
			n, err := strconv.Atoi(item)
			if err != nil || n <= 0 {
				return fmt.Errorf("must contain only positive integers")
			}
		}
	case "cera.items":
		if _, err := decodeCeraItems(value); err != nil {
			return err
		}
	}
	return nil
}
