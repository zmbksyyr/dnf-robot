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
		{name: "login", content: loginHTML, required: []string{"Robot Login", `action="/login"`, "{{if .Error}}"}},
		{name: "index", content: indexHTML, required: []string{"TW Robot Web", appCSSPlaceholder, i18nJSPlaceholder, appJSPlaceholder, `id="languageButton"`, `id="partyCompatButton"`, `id="compatButton"`}},
		{name: "css", content: appCSS, required: []string{":root{", ".service-lights", ".diagrow"}},
		{name: "i18n", content: i18nJS, required: []string{"I18N_MESSAGES", "tw_language", "toggleLanguage", "currentLanguage=localStorage.getItem(I18N_STORAGE_KEY)==='zh'?'zh':'en'"}},
		{name: "javascript", content: appJS, required: []string{"async function api(", "openPartyCompatDialog", "openCompatDialog", "openDiagnosticsDialog", "restartRobot", "autoMailNotify"}},
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
