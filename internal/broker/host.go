package broker

import (
	"cmp"
	"errors"
	"net"
)

// Host holds the host-side compose values. The container learns them only
// to show them and to print the setup link; it cannot change them.
type Host struct {
	BindAddress   string `json:"bind_address"`
	PanelPort     string `json:"panel_port"`
	OrcaPort      string `json:"orca_port"`
	PUID          string `json:"puid"`
	PGID          string `json:"pgid"`
	WorkspacesDir string `json:"workspaces_dir"`
}

// HostFromEnv reads the BERTH_HOST_* variables compose passes through.
func HostFromEnv(getenv func(string) string) Host {
	return Host{
		BindAddress:   cmp.Or(getenv("BERTH_HOST_BIND_ADDRESS"), "127.0.0.1"),
		PanelPort:     cmp.Or(getenv("BERTH_HOST_PANEL_PORT"), "7690"),
		OrcaPort:      cmp.Or(getenv("BERTH_HOST_ORCA_PORT"), "6768"),
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

// ErrOrcaAddressNeeded means the host binds every address, so no client
// could dial the default pairing address.
var ErrOrcaAddressNeeded = errors.New("BIND_ADDRESS is a wildcard, so Orca apps cannot dial it; set frontends.orca.pairing_address to an address they reach")

// OrcaAddress is the address Orca apps are told to dial: override when set,
// else the bind address and the host Orca port. A loopback bind address
// still pairs a client on this host, with a warning that no other device
// can.
func (h Host) OrcaAddress(override string) (addr, warning string, err error) {
	if override != "" {
		return override, "", nil
	}
	ip := net.ParseIP(h.BindAddress)
	if h.BindAddress == "" || (ip != nil && ip.IsUnspecified()) {
		return "", "", ErrOrcaAddressNeeded
	}
	addr = net.JoinHostPort(h.BindAddress, h.OrcaPort)
	if h.BindAddress == "localhost" || (ip != nil && ip.IsLoopback()) {
		warning = "Orca is published on " + h.BindAddress + ", so only this host can pair; to pair other devices, set BIND_ADDRESS to a private address or frontends.orca.pairing_address to one they reach"
	}
	return addr, warning, nil
}
