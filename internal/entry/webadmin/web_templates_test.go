package webadmin

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedWebAssetsContainRequiredContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		required []string
	}{
		{name: "login", content: loginHTML, required: []string{"Robot Web", `action="/login"`, "{{if .Error}}", i18nJSPlaceholder, `id="languageButton"`}},
		{name: "index", content: indexHTML, required: []string{"TW Robot Web", appCSSPlaceholder, i18nJSPlaceholder, appJSPlaceholder, `id="languageButton"`, `id="partyCompatButton"`, `id="compatButton"`}},
		{name: "css", content: appCSS, required: []string{":root{", ".service-lights", ".diagrow", ".market-policy-select"}},
		{name: "i18n", content: i18nJS, required: []string{"I18N_MESSAGES", "tw_language", "toggleLanguage", "currentLanguage=localStorage.getItem(I18N_STORAGE_KEY)==='zh'?'zh':'en'", "auto.shout_interval", "喊话间隔", "validation.shout_interval", "market.section_status", "market.price_range_policy", "market.allowed_rarities", "上架稀有度（0-9）", "范围外回收概率"}},
		{name: "javascript", content: appJS, required: []string{"async function api(", "openPartyCompatDialog", "openCompatDialog", "openDiagnosticsDialog", "restartRobot", "autoMailNotify", "autoShoutMin", "autoShoutMax", "auto.auto_shout_interval_min_sec", "auto.auto_shout_interval_max_sec", "marketAllowedRarities", "normalizeRarityDigits", "allowed_rarities", "marketEquipmentLevelMin", "equipment_trade_policy", "material_trade_policy", "marketApplyListingConfig"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.TrimSpace(tt.content) == "" {
				t.Fatal("embedded asset is empty")
			}
			for _, required := range tt.required {
				if !strings.Contains(tt.content, required) {
					t.Errorf("embedded asset is missing %q", required)
				}
			}
		})
	}
}

func TestIndexTemplateInlinesEmbeddedAssets(t *testing.T) {
	var rendered bytes.Buffer
	if err := cleanIndexTemplate.Execute(&rendered, nil); err != nil {
		t.Fatalf("execute index template: %v", err)
	}
	page := rendered.String()
	if strings.Contains(page, appCSSPlaceholder) || strings.Contains(page, i18nJSPlaceholder) || strings.Contains(page, appJSPlaceholder) {
		t.Fatal("rendered index still contains an asset placeholder")
	}
	for _, want := range []string{
		"<style>\n" + trimAssetTerminator(appCSS) + "\n</style>",
		"<script>\n" + trimAssetTerminator(i18nJS) + "\n</script>",
		"<script>\n" + trimAssetTerminator(appJS) + "\n</script>",
	} {
		if !strings.Contains(page, want) {
			t.Fatal("rendered index does not contain an embedded asset")
		}
	}
	if strings.Index(page, trimAssetTerminator(i18nJS)) > strings.Index(page, trimAssetTerminator(appJS)) {
		t.Fatal("i18n script must load before the application script")
	}
}

func TestLoginTemplateInlinesI18nAsset(t *testing.T) {
	var rendered bytes.Buffer
	if err := cleanLoginTemplate.Execute(&rendered, nil); err != nil {
		t.Fatalf("execute login template: %v", err)
	}
	page := rendered.String()
	if strings.Contains(page, i18nJSPlaceholder) {
		t.Fatal("rendered login still contains the i18n asset placeholder")
	}
	if !strings.Contains(page, trimAssetTerminator(i18nJS)) {
		t.Fatal("rendered login does not contain the embedded i18n asset")
	}
}

func TestI18nLocalesHaveMatchingKeys(t *testing.T) {
	parts := strings.SplitN(i18nJS, "\n},\nzh:{\n", 2)
	if len(parts) != 2 {
		t.Fatal("cannot split English and Chinese locale tables")
	}
	keyPattern := regexp.MustCompile(`'([a-zA-Z0-9_.]+)':`)
	keys := func(content string) map[string]bool {
		out := make(map[string]bool)
		for _, match := range keyPattern.FindAllStringSubmatch(content, -1) {
			out[match[1]] = true
		}
		return out
	}
	zhTable := strings.SplitN(parts[1], "\n}};", 2)[0]
	enKeys, zhKeys := keys(parts[0]), keys(zhTable)
	for key := range enKeys {
		if !zhKeys[key] {
			t.Errorf("Chinese locale is missing %q", key)
		}
	}
	for key := range zhKeys {
		if !enKeys[key] {
			t.Errorf("English locale is missing %q", key)
		}
	}
}

func TestRobotJobNamesAlwaysUseChineseCatalog(t *testing.T) {
	if !strings.Contains(appJS, "const table=I18N_MESSAGES.zh||{}") {
		t.Fatal("robot job display is not fixed to the Chinese catalog")
	}
}

func TestDiagnosticsDialogKeepsRawEnglishText(t *testing.T) {
	if !strings.Contains(appJS, "showModal('Diagnostics',body,'<button onclick=\"closeModal()\">Close</button>','diagnostics',false)") {
		t.Fatal("diagnostics dialog is not configured to bypass translation")
	}
	for _, want := range []string{"Start Party Debug", "Stop &amp; Analyze", "partyDebugStatus", "partyDebugActions", "Party Debug Result", "party-debug-result"} {
		if !strings.Contains(appJS, want) {
			t.Fatalf("party debug UI is missing %q", want)
		}
	}
	if strings.Contains(appJS, "partyDebugPanelHTML") || strings.Contains(appCSS, "background:#0f172a") {
		t.Fatal("Diagnostics still contains the old persistent black Party debug panel")
	}
}

func TestSchedulerAlwaysUsesCompactEnglish(t *testing.T) {
	for _, want := range []string{
		`class="scheduler" data-i18n-skip`,
		`<div class="k">Policy</div>`,
		`<div class="k">Attach</div>`,
		"i18nEnglishFormat('scheduler.attach_value'",
		"'{rate}/s · b{batch}'",
		"node.parentElement?.closest('[data-i18n-skip]')",
	} {
		if !strings.Contains(indexHTML+appJS+i18nJS, want) {
			t.Fatalf("scheduler compact English behavior is missing %q", want)
		}
	}
}

func TestRequestedChineseLabelsAndDialogWidths(t *testing.T) {
	for _, want := range []string{
		"'action.market':'拍卖'",
		"'common.cast':'释放'",
		"auto-form",
		"party-account-input",
		".party-account-input{width:124px!important}",
	} {
		if !strings.Contains(appCSS+appJS+i18nJS, want) {
			t.Fatalf("requested web label or width is missing %q", want)
		}
	}
}

func TestMarketDialogUsesCompactAlignedLayout(t *testing.T) {
	for _, want := range []string{
		"dialog.market{width:min(600px,96vw)",
		"dialog.market .formgrid>label{white-space:nowrap}",
		"dialog.market .market-range",
		"showModal('Market',body,foot,'market')",
	} {
		if !strings.Contains(appCSS+appJS, want) {
			t.Fatalf("market dialog is missing compact layout rule %q", want)
		}
	}
}

func TestStoreColumnStaysEnglish(t *testing.T) {
	if strings.Contains(indexHTML, `data-i18n="robots.store"`) {
		t.Fatal("robot Store header still participates in language switching")
	}
	if !strings.Contains(indexHTML, `<th data-i18n-skip>Store</th>`) {
		t.Fatal("robot Store header is not protected from text-node translation")
	}
	if !strings.Contains(appJS, "span.textContent=i18nEnglishFormat('status.'+store)") {
		t.Fatal("robot Store values are not fixed to English")
	}
}

func TestAutoDialogUsesStandardFooterWithoutWrapping(t *testing.T) {
	for _, want := range []string{
		`const foot='<button onclick="submitAuto(null)">Save settings</button>`,
		"showModal('Auto',body,foot)",
		".auto-form>input[type=number]{width:120px}",
		".auto-option{white-space:nowrap}",
		"grid-template-columns:68px 16px 68px max-content",
		`<span>~</span><input id="autoShoutMax"`,
	} {
		if !strings.Contains(appCSS+appJS, want) {
			t.Fatalf("Auto dialog layout is missing %q", want)
		}
	}
}

func TestPortsDialogUsesStandardFooter(t *testing.T) {
	for _, want := range []string{
		`const foot='<button onclick="submitGamePort()">Save Ports</button>`,
		"showModal('Ports',body,foot,'ports')",
		"dialog.ports{width:min(320px,96vw)",
		"dialog.ports .formgrid{grid-template-columns:100px 92px}",
		`const ports=endpoint.ports||{};const body='<div class="formgrid"><label>Game</label>`,
	} {
		if !strings.Contains(appCSS+appJS, want) {
			t.Fatalf("Ports dialog standard footer is missing %q", want)
		}
	}
	for _, removed := range []string{`id="gameHost"`, `id="loginIP"`, `id="auctionHost"`, `id="pointHost"`, `id="relayHost"`, `id="serviceRoot"`, `id="serviceRunScript"`} {
		if strings.Contains(appJS, removed) {
			t.Fatalf("Ports dialog still exposes non-port field %q", removed)
		}
	}
}

func TestMarketFieldsUseOneCompactAlignment(t *testing.T) {
	for _, want := range []string{
		"dialog.market .formgrid{grid-template-columns:160px minmax(0,1fr)}",
		"grid-template-columns:minmax(0,150px) max-content",
		"grid-template-columns:68px 16px 68px max-content",
		"marketUpgradeMax",
		"marketStackSizes",
	} {
		if !strings.Contains(appCSS+appJS, want) {
			t.Fatalf("compact market alignment is missing %q", want)
		}
	}
	if strings.Index(appJS, "marketUpgradeMax") > strings.Index(appJS, "marketStackSizes") {
		t.Fatal("stack sizes must appear below upgrade in the market dialog")
	}
}

func TestMarketDialogHasExplicitRebuildWithoutAutoSave(t *testing.T) {
	for _, want := range []string{
		`id="modalSaveButton"`,
		"submitMarketSettings()",
		"marketApplyListingConfig",
		"style==='market'?'':'none'",
	} {
		if !strings.Contains(indexHTML+appJS, want) {
			t.Fatalf("market save behavior is missing %q", want)
		}
	}
	for _, hidden := range []string{"marketCycleSeconds", "marketAutoConcurrent", "marketRestockMaxActions", "marketCollectMaxActions", "marketInRangeProbability"} {
		if strings.Contains(appJS, hidden) {
			t.Fatalf("market dialog still exposes runtime field %q", hidden)
		}
	}
	if strings.Contains(appJS, "function autoSaveMarketConfig()") {
		t.Fatal("market dialog must not auto-save")
	}
	if strings.Contains(appJS, "Only these rarity digits will be listed; missing rarity is treated as 0</div>") {
		t.Fatal("market rarity field still renders its explanatory note")
	}
}

func TestLoginTemplateEscapesError(t *testing.T) {
	const loginError = `<script>alert("bad")</script>`
	var rendered bytes.Buffer
	if err := cleanLoginTemplate.Execute(&rendered, map[string]string{"Error": loginError}); err != nil {
		t.Fatalf("execute login template: %v", err)
	}
	page := rendered.String()
	if strings.Contains(page, loginError) || !strings.Contains(page, "&lt;script&gt;") {
		t.Fatalf("login error was not HTML-escaped: %q", page)
	}
}
