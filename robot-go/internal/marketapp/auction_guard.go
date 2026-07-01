package marketapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultDFGameRJSPath = "/dp2/df_game_r.js"

const auctionSearchGuardBegin = "// DP2_AUCTION_SEARCH_HOOK_GUARD_BEGIN"
const auctionSearchGuardEnd = "// DP2_AUCTION_SEARCH_HOOK_GUARD_END"

const auctionSearchGuardSource = auctionSearchGuardBegin + `
(function () {
    var root = (typeof globalThis !== 'undefined') ? globalThis : this;
    var key = '__dp2_auction_search_hook_guard_v1__';
    if (root[key]) {
        return;
    }
    root[key] = true;

    var blocked = {};
    blocked[ptr('0x084D75BC').toString().toLowerCase()] = true;

    var rawReplace = Interceptor.replace.bind(Interceptor);
    var rawRevert = Interceptor.revert.bind(Interceptor);

    function addrOf(target) {
        try {
            return ptr(target).toString().toLowerCase();
        } catch (e) {
            try {
                return target.toString().toLowerCase();
            } catch (_) {
                return '';
            }
        }
    }

    Interceptor.replace = function (target, replacement) {
        var addr = addrOf(target);
        if (blocked[addr]) {
            try {
                rawRevert(target);
                Interceptor.flush();
            } catch (e) {
            }
            console.log('[dp2 guard] blocked auction search Interceptor.replace at ' + addr);
            return;
        }
        return rawReplace(target, replacement);
    };

    console.log('[dp2 guard] auction search hook guard installed');
})();
` + auctionSearchGuardEnd + `

`

func (a *App) InstallAuctionSearchGuard(req AuctionSearchGuardRequest) (AuctionSearchGuardResult, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = defaultDFGameRJSPath
	}
	result := AuctionSearchGuardResult{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Contains(data, []byte(auctionSearchGuardBegin)) {
		result.Installed = true
		result.Message = "auction search hook guard already installed"
		a.appendLog(LogEvent{Type: "auction_guard", Status: "exists", Message: path})
		return result, nil
	}
	backup := fmt.Sprintf("%s.bak_auction_guard_%s", path, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		return result, fmt.Errorf("prepare backup dir: %w", err)
	}
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return result, fmt.Errorf("backup %s: %w", backup, err)
	}
	next := append([]byte(auctionSearchGuardSource), data...)
	if err := os.WriteFile(path, next, 0644); err != nil {
		return result, fmt.Errorf("write %s: %w", path, err)
	}
	result.Backup = backup
	result.Installed = true
	result.Changed = true
	result.Message = "auction search hook guard installed; restart df_game_r to apply"
	a.appendLog(LogEvent{Type: "auction_guard", Status: "installed", Message: fmt.Sprintf("%s backup=%s", path, backup)})
	return result, nil
}
