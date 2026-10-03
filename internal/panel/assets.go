package panel

import (
	"io/fs"

	"github.com/berth-ai/berth/web"
)

// assetTypes are the files /static/ serves, by extension: what the Vite
// build emits. Anything else is a 404.
var assetTypes = map[string]string{
	".js":  "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8",
}

// builtAssets is the embedded frontend, web/dist without its prefix.
func builtAssets() fs.FS {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err) // fs.Sub fails only for an invalid path
	}
	return sub
}
