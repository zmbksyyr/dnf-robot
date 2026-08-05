package tcpapi

import (
	"reflect"
	"testing"
)

func TestManualMarketTargetsTreatsEmptyAsBothMarkets(t *testing.T) {
	if got, want := manualMarketTargets("  "), []string{"auction", "cera"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manualMarketTargets(empty)=%v, want %v", got, want)
	}
	if got, want := manualMarketTargets("auction"), []string{"auction"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manualMarketTargets(auction)=%v, want %v", got, want)
	}
}
