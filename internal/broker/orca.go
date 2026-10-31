package broker

import (
	"github.com/berth-ai/berth/internal/config"
)

// checkOrca refuses switching Orca on in an image that does not ship it.
// A config that already has it on stays, so the switch can be turned off.
func (b *Broker) checkOrca(cur, next config.Config) error {
	if !next.Frontends.Orca.Enabled || cur.Frontends.Orca.Enabled || b.OrcaInstalled == nil || b.OrcaInstalled() {
		return nil
	}
	return &config.ValidationError{Fields: []config.FieldError{{Field: "frontends.orca.enabled", Message: "Orca is not installed in this image"}}}
}
