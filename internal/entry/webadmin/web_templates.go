package webadmin

import (
	_ "embed"
	"html/template"
	"strings"
)

const (
	appCSSPlaceholder = "<!-- APP_CSS -->"
	appJSPlaceholder  = "<!-- APP_JS -->"
)

//go:embed assets/login.html
var loginHTML string

//go:embed assets/index.html
var indexHTML string

//go:embed assets/app.css
var appCSS string

//go:embed assets/app.js
var appJS string

var cleanLoginTemplate = template.Must(template.New("clean_login").Parse(trimAssetTerminator(loginHTML)))
var cleanIndexTemplate = template.Must(template.New("clean_index").Parse(inlineIndexAssets()))

func inlineIndexAssets() string {
	if strings.Count(indexHTML, appCSSPlaceholder) != 1 || strings.Count(indexHTML, appJSPlaceholder) != 1 {
		panic("webadmin: index asset placeholders must each occur exactly once")
	}
	return strings.NewReplacer(
		appCSSPlaceholder, "<style>\n"+trimAssetTerminator(appCSS)+"\n</style>",
		appJSPlaceholder, "<script>\n"+trimAssetTerminator(appJS)+"\n</script>",
	).Replace(trimAssetTerminator(indexHTML))
}

func trimAssetTerminator(content string) string {
	content = strings.TrimSuffix(content, "\n")
	return strings.TrimSuffix(content, "\r")
}
