// Package web exports the embedded static web assets.
package web

import "embed"

//go:embed index.html metrics-history.html static
var FS embed.FS
