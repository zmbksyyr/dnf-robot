//go:build !linux

package marketapp

import "fmt"

func (a *App) startAuctionBeforeLoad(marketServiceSpec) error {
	return fmt.Errorf("auction exec patch requires Linux")
}
