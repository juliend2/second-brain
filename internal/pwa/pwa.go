// Package pwa embeds the mobile second-brain web app (index, styles, script,
// manifest, service worker, icons) so the API binary is self-contained.
package pwa

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// FS returns the PWA files with static/ as the root.
func FS() fs.FS {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // static is embedded; cannot fail
	}
	return sub
}
