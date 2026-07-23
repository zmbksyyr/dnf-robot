package webadmin

import (
	"bytes"
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
		{name: "index", content: indexHTML, required: []string{"TW Robot Web", appCSSPlaceholder, appJSPlaceholder, `id="partyCompatButton"`, `id="compatButton"`}},
		{name: "css", content: appCSS, required: []string{":root{", ".service-lights", ".diagrow"}},
		{name: "javascript", content: appJS, required: []string{"async function api(", "openPartyCompatDialog", "openCompatDialog", "openDiagnosticsDialog", "restartRobot"}},
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
	if strings.Contains(page, appCSSPlaceholder) || strings.Contains(page, appJSPlaceholder) {
		t.Fatal("rendered index still contains an asset placeholder")
	}
	for _, want := range []string{
		"<style>\n" + trimAssetTerminator(appCSS) + "\n</style>",
		"<script>\n" + trimAssetTerminator(appJS) + "\n</script>",
	} {
		if !strings.Contains(page, want) {
			t.Fatal("rendered index does not contain an embedded asset")
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
