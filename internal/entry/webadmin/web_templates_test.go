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
		{name: "css", content: appCSS, required: []string{":root{", ".service-lights", ".diagrow"}},
		{name: "i18n", content: i18nJS, required: []string{"I18N_MESSAGES", "tw_language", "toggleLanguage", "currentLanguage=localStorage.getItem(I18N_STORAGE_KEY)==='zh'?'zh':'en'", "market.section_status", "market.price_range_policy", "market.allowed_rarities", "上架稀有度（0-9）", "范围外回收概率"}},
		{name: "javascript", content: appJS, required: []string{"async function api(", "openPartyCompatDialog", "openCompatDialog", "openDiagnosticsDialog", "restartRobot", "autoMailNotify", "marketAllowedRarities", "normalizeRarityDigits", "allowed_rarities", "marketEquipmentLevelMin", "marketRestockMaxActions", "marketCollectMaxActions", "auto_max_actions", "collector_max_actions"}},
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
	if !strings.Contains(appJS, "showModal('Diagnostics',body,'<button onclick=\"closeModal()\">Close</button>','',false)") {
		t.Fatal("diagnostics dialog is not configured to bypass translation")
	}
}

func TestMarketDialogUsesCompactAlignedLayout(t *testing.T) {
	for _, want := range []string{
		"dialog.market{width:min(820px,96vw)",
		"dialog.market .formgrid>label{white-space:nowrap}",
		"dialog.market .market-range",
		"showModal('Market',body,foot,'market')",
	} {
		if !strings.Contains(appCSS+appJS, want) {
			t.Fatalf("market dialog is missing compact layout rule %q", want)
		}
	}
}

func TestMarketDialogHasExplicitSaveAndKeepsAutoSave(t *testing.T) {
	for _, want := range []string{
		`id="modalSaveButton"`,
		"submitMarketConfigExplicit()",
		"function autoSaveMarketConfig()",
		"style==='market'?'':'none'",
	} {
		if !strings.Contains(indexHTML+appJS, want) {
			t.Fatalf("market save behavior is missing %q", want)
		}
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
