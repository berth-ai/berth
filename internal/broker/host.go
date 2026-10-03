package broker

import (
	"cmp"
	"net"
)

// Host holds the host-side compose values. The container learns them only
// to show them and to print the setup link; it cannot change them.
type Host struct {
	BindAddress   string `json:"bind_address"`
	PanelPort     string `json:"panel_port"`
	PUID          string `json:"puid"`
	PGID          string `json:"pgid"`
	WorkspacesDir string `json:"workspaces_dir"`
}

// HostFromEnv reads the BERTH_HOST_* variables compose passes through.
func HostFromEnv(getenv func(string) string) Host {
	return Host{
		BindAddress:   cmp.Or(getenv("BERTH_HOST_BIND_ADDRESS"), "127.0.0.1"),
		PanelPort:     cmp.Or(getenv("BERTH_HOST_PANEL_PORT"), "7690"),
		PUID:          cmp.Or(getenv("BERTH_HOST_PUID"), "1000"),
		PGID:          cmp.Or(getenv("BERTH_HOST_PGID"), "1000"),
		WorkspacesDir: cmp.Or(getenv("BERTH_HOST_WORKSPACES_DIR"), "./workspaces"),
	}
}

// SetupURL is the link that completes setup. A wildcard bind address is
// shown as localhost, since the operator knows which of the host's
// addresses they use.
func (h Host) SetupURL(token string) string {
	host := h.BindAddress
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, h.PanelPort) + "/setup#token=" + token
}
