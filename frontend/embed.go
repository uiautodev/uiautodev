// Package frontend embeds the built frontend so binaries can serve it without
// copying the build output into their own package directories.
package frontend

import "embed"

//go:embed all:build
var Assets embed.FS
