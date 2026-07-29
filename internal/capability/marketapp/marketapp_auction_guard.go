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
    var key = '__dp2_auction_search_hook_guard_v2__';
    if (root[key]) {
        return;
    }
    root[key] = true;

    var targetKey = ptr('0x084D75BC').toString().toLowerCase();
    var rawReplace = Interceptor.replace.bind(Interceptor);
    var rawAttach = Interceptor.attach.bind(Interceptor);

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

    function installSafeSearch(target) {
        rawAttach(target, {
            onEnter: function (args) {
                try {
                    if (typeof G_CDataManager !== 'function' ||
                        typeof CDataManager_find_item !== 'function' ||
                        typeof CItem_getItemGroupName !== 'function' ||
                        typeof api_get_jewel_socket_data !== 'function') {
                        return;
                    }
                    var src = args[2];
                    if (src.isNull()) {
                        return;
                    }
                    var count = src.add(5).readU8();
                    if (count === 0 || count > 100) {
                        return;
                    }

                    // Validate the whole 137-byte layout before writing anything.
                    // Unknown packet layouts fall back to the native search path.
                    var records = [];
                    for (var i = 0; i < count; i++) {
                        var itemId = src.add(54 + 137 * i).readU32();
                        if (itemId === 0) {
                            return;
                        }
                        var item = CDataManager_find_item(G_CDataManager(), itemId);
                        if (item.isNull()) {
                            return;
                        }
                        records.push({
                            index: i,
                            group: CItem_getItemGroupName(item)
                        });
                    }

                    for (var n = 0; n < records.length; n++) {
                        var record = records[n];
                        if (record.group <= 0 || record.group >= 59) {
                            continue;
                        }
                        var socketData = api_get_jewel_socket_data(
                            mysql_frida,
                            src.add(76 + 137 * record.index).readU32()
                        );
                        if (socketData.isNull()) {
                            continue;
                        }
                        Memory.copy(src.add(106 + 137 * record.index), socketData, 30);
                    }
                } catch (e) {
                    console.log('[dp2 guard] safe auction search skipped: ' + e);
                }
            }
        });
        console.log('[dp2 guard] safe auction search hook installed at ' + addrOf(target));
    }

    Interceptor.replace = function (target, replacement) {
        if (addrOf(target) !== targetKey) {
            return rawReplace(target, replacement);
        }

        // The dp2 helpers are initialized by the time its search replacement is
        // requested. Keep the native dispatcher and attach only preprocessing.
        Interceptor.replace = rawReplace;
        try {
            installSafeSearch(target);
        } catch (e) {
            console.log('[dp2 guard] safe auction search install failed: ' + e);
        }
        return;
    };

    console.log('[dp2 guard] waiting for auction search hook');
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
		if os.IsNotExist(err) {
			result.Message = "dp2 script not found; auction guard is not applicable"
			a.appendLog(LogEvent{Type: "auction_guard", Status: marketLogStatusSkipped, Message: path})
			return result, nil
		}
		return result, fmt.Errorf("read %s: %w", path, err)
	}
	next, changed, err := upsertAuctionSearchGuard(data)
	if err != nil {
		return result, fmt.Errorf("update %s: %w", path, err)
	}
	if !changed {
		result.Installed = true
		result.Message = "safe auction search hook already installed"
		a.appendLog(LogEvent{Type: "auction_guard", Status: marketLogStatusExists, Message: path})
		return result, nil
	}
	backup := fmt.Sprintf("%s.bak_auction_guard_%s", path, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		return result, fmt.Errorf("prepare backup dir: %w", err)
	}
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return result, fmt.Errorf("backup %s: %w", backup, err)
	}
	if err := os.WriteFile(path, next, 0644); err != nil {
		return result, fmt.Errorf("write %s: %w", path, err)
	}
	result.Backup = backup
	result.Installed = true
	result.Changed = true
	result.Message = "safe auction search hook installed; restart df_game_r to apply"
	a.appendLog(LogEvent{Type: "auction_guard", Status: marketLogStatusInstalled, Message: fmt.Sprintf("%s backup=%s", path, backup)})
	return result, nil
}

func upsertAuctionSearchGuard(data []byte) ([]byte, bool, error) {
	clean := append([]byte(nil), data...)
	begin := []byte(auctionSearchGuardBegin)
	end := []byte(auctionSearchGuardEnd)
	for {
		start := bytes.Index(clean, begin)
		if start < 0 {
			if bytes.Contains(clean, end) {
				return nil, false, fmt.Errorf("guard end marker exists without begin marker")
			}
			break
		}
		finishOffset := bytes.Index(clean[start+len(begin):], end)
		if finishOffset < 0 {
			return nil, false, fmt.Errorf("guard begin marker exists without end marker")
		}
		finish := start + len(begin) + finishOffset + len(end)
		finish = consumeLineEndings(clean, finish, 2)
		clean = append(clean[:start], clean[finish:]...)
	}
	next := make([]byte, 0, len(auctionSearchGuardSource)+len(clean))
	next = append(next, auctionSearchGuardSource...)
	next = append(next, clean...)
	return next, !bytes.Equal(next, data), nil
}

func consumeLineEndings(data []byte, offset, max int) int {
	for i := 0; i < max && offset < len(data); i++ {
		switch data[offset] {
		case '\n':
			offset++
		case '\r':
			offset++
			if offset < len(data) && data[offset] == '\n' {
				offset++
			}
		default:
			return offset
		}
	}
	return offset
}
