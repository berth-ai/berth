module github.com/berth-ai/berth

go 1.27

toolchain go1.27.3

// web/node_modules holds npm packages, some of which ship stray .go files.
ignore ./web/node_modules

require (
	github.com/creack/pty v1.1.24
	github.com/pelletier/go-toml/v2 v2.4.3
	golang.org/x/crypto v0.58.0
	rsc.io/qr v0.2.0
)

require golang.org/x/sys v0.49.0 // indirect
