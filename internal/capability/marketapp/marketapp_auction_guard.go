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
const auctionSearchGuardReplace = "DP2_AUCTION_SEARCH_HOOK_GUARD_REPLACE"

const auctionSearchGuardSource = auctionSearchGuardBegin + `
function DP2_AUCTION_SEARCH_HOOK_GUARD_REPLACE(target, ignoredReplacement) {
    var root = (typeof globalThis !== 'undefined') ? globalThis : this;
    var key = '__dp2_auction_search_hook_guard_v5__';
    if (root[key]) {
        return;
    }
    root[key] = true;

    var targetKey = target.toString().toLowerCase();
    var nativeSearch = new NativeFunction(
        target,
        'int',
        ['pointer', 'pointer', 'pointer', 'int'],
        { abi: 'sysv' }
    );
    var replacement = new NativeCallback(function (dispatcher, user, src, a4) {
        try {
            if (!src.isNull() &&
                typeof G_CDataManager === 'function' &&
                typeof CDataManager_find_item === 'function' &&
                typeof CItem_getItemGroupName === 'function' &&
                typeof api_get_jewel_socket_data === 'function') {
                var count = src.add(5).readU8();
                if (count <= 100) {
                    for (var i = 0; i < count; i++) {
                        var itemId = src.add(54 + 137 * i).readU32();
                        if (itemId === 0) {
                            continue;
                        }
                        var item = CDataManager_find_item(G_CDataManager(), itemId);
                        if (item.isNull()) {
                            continue;
                        }
                        var group = CItem_getItemGroupName(item);
                        if (group <= 0 || group >= 59) {
                            continue;
                        }
                        var equipmentId = src.add(76 + 137 * i).readU32();
                        var socketData = api_get_jewel_socket_data(mysql_frida, equipmentId);

                        // api_get_jewel_socket_data always allocates a buffer.
                        // Byte zero is its actual "record exists" indicator.
                        // Preserve native bytes for normal/untracked equipment.
                        if (socketData.isNull() || socketData.add(0).readU8() === 0) {
                            continue;
                        }
                        Memory.copy(src.add(106 + 137 * i), socketData, 30);
                    }
                }
            }
        } catch (e) {
            console.log('[dp2 guard] socket overlay skipped: ' + e);
        }
        return nativeSearch(dispatcher, user, src, a4);
    }, 'int', ['pointer', 'pointer', 'pointer', 'int']);

    Interceptor.replace(target, replacement);
    console.log('[dp2 guard] compatible auction search installed at ' + targetKey);
}
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
		result.Message = "auction search guard already installed"
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
	result.Message = "auction search guard installed; restart df_game_r to apply"
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
	var err error
	clean, err = rewriteAuctionSearchReplacement(clean)
	if err != nil {
		return nil, false, err
	}
	next := make([]byte, 0, len(auctionSearchGuardSource)+len(clean))
	next = append(next, auctionSearchGuardSource...)
	next = append(next, clean...)
	return next, !bytes.Equal(next, data), nil
}

func rewriteAuctionSearchReplacement(data []byte) ([]byte, error) {
	lower := bytes.ToLower(data)
	target := []byte("0x084d75bc")
	original := []byte("interceptor.replace")
	replacement := []byte(strings.ToLower(auctionSearchGuardReplace))
	originalAt := -1
	replacementAt := -1

	for offset := 0; offset < len(lower); {
		rel := bytes.Index(lower[offset:], target)
		if rel < 0 {
			break
		}
		targetAt := offset + rel
		start := targetAt - 128
		if start < 0 {
			start = 0
		}
		prefix := lower[start:targetAt]
		if at := bytes.LastIndex(prefix, original); at >= 0 && directAuctionTargetCall(prefix[at+len(original):]) {
			if originalAt >= 0 {
				return nil, fmt.Errorf("multiple auction search replacements found")
			}
			originalAt = start + at
		}
		if at := bytes.LastIndex(prefix, replacement); at >= 0 && directAuctionTargetCall(prefix[at+len(replacement):]) {
			if replacementAt >= 0 {
				return nil, fmt.Errorf("multiple guarded auction search replacements found")
			}
			replacementAt = start + at
		}
		offset = targetAt + len(target)
	}

	if originalAt >= 0 && replacementAt >= 0 {
		return nil, fmt.Errorf("mixed original and guarded auction search replacements found")
	}
	if replacementAt >= 0 {
		return data, nil
	}
	if originalAt < 0 {
		return nil, fmt.Errorf("auction search replacement at 0x084D75BC not found")
	}

	next := make([]byte, 0, len(data)-len(original)+len(auctionSearchGuardReplace))
	next = append(next, data[:originalAt]...)
	next = append(next, auctionSearchGuardReplace...)
	next = append(next, data[originalAt+len(original):]...)
	return next, nil
}

func directAuctionTargetCall(between []byte) bool {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		default:
			return r
		}
	}, string(between))
	return compact == "(ptr(" || compact == "(ptr('" || compact == "(ptr(\""
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
