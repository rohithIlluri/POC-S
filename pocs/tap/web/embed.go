// Package web embeds the browser demo that tapd serves at /.
package web

import "embed"

//go:embed index.html style.css app.js protocol.js admin.html
var FS embed.FS
