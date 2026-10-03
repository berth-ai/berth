// Package broker is the root side of the web panel.
package broker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Exec runs berth with args and returns its exit code. err is non-nil only
// when the process could not be run at all.
type Exec func(ctx context.Context, args []string, stdout, stderr io.Writer) (exit int, err error)

func errorJSON(code, message string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
	return raw
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
