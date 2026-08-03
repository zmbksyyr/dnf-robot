package marketapp

import "testing"

func TestValidateExternalMarketName(t *testing.T) {
	for _, valid := range []string{"auction", " cera "} {
		if _, err := ValidateExternalMarketName(valid); err != nil {
			t.Fatalf("ValidateExternalMarketName(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "gold", "point", "unknown"} {
		if _, err := ValidateExternalMarketName(invalid); err == nil {
			t.Fatalf("ValidateExternalMarketName(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestRequestedRestockMarketsRejectsAliases(t *testing.T) {
	for _, invalid := range []string{"", "gold", "point", "unknown"} {
		if _, _, _, err := requestedRestockMarkets(invalid); err == nil {
			t.Fatalf("requestedRestockMarkets(%q) unexpectedly succeeded", invalid)
		}
	}
}
