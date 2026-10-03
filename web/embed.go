// Package web holds the panel's built frontend. Run pnpm build in this
// directory to fill dist; an unbuilt tree holds only dist/.gitkeep, and the
// panel then answers its pages with 503.
package web

import "embed"

// Dist is the Vite build: pages at dist/*.html, hashed files in dist/assets.
//
//go:embed all:dist
var Dist embed.FS
