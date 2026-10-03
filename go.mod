module github.com/berth-ai/berth

go 1.27

toolchain go1.27.1

// web/node_modules holds npm packages, some of which ship stray .go files.
ignore ./web/node_modules

require (
	github.com/pelletier/go-toml/v2 v2.4.3
	golang.org/x/crypto v0.57.0
)

require golang.org/x/sys v0.48.0 // indirect
