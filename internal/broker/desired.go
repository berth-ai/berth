package broker

import (
	"github.com/berth-ai/bert/internal/config"
	"github.com/berth-ai/bert/internal/supervisor"
)

// Desired maps a config to the services that should run, in start order. The
// panel always runs and gets TZ as a soft setting, so a time zone change
// never logs everyone out. The frontends get it as a setting, so they
// restart and new shells pick it up. Outside normal mode (setup, recovery)
// only the panel runs.
func Desired(cfg config.Config, mode string) []supervisor.Spec {
	tz := cfg.Runtime.TZ
	specs := []supervisor.Spec{{Name: "panel", Env: map[string]string{}, Soft: map[string]string{"TZ": tz}}}
	if mode != ModeNormal {
		return specs
	}
	if cfg.Frontends.Terminal.Enabled {
		specs = append(specs, supervisor.Spec{Name: "ttyd", Env: map[string]string{"TZ": tz}, Soft: map[string]string{}})
	}
	if cfg.Frontends.IDE.Enabled {
		specs = append(specs, supervisor.Spec{Name: "code_server", Env: map[string]string{"TZ": tz}, Soft: map[string]string{}})
	}
	return specs
}
