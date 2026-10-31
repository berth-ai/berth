package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// CheckPairingAddress returns why s cannot be the address Orca apps dial, or
// "" when it can. Empty is valid and means the host default. A bare host or
// IP, host:port, or a ws:// or wss:// URL is accepted.
func CheckPairingAddress(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 255 || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return "must be a host name, IP address, host:port, or ws:// or wss:// URL with no spaces"
	}
	host := s
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
			return "must use ws:// or wss:// when it is a URL"
		}
		if u.User != nil || u.Hostname() == "" {
			return "must be a ws:// or wss:// URL with a host and no user name"
		}
		if p := u.Port(); p != "" {
			if msg := checkPort(p); msg != "" {
				return msg
			}
		}
		host = u.Hostname()
	} else if h, p, err := net.SplitHostPort(s); err == nil {
		if msg := checkPort(p); msg != "" {
			return msg
		}
		host = h
	} else if strings.Count(s, ":") == 1 {
		return "must be host:port with a port between 1 and 65535"
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return "must name a host"
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLoopback():
			return "must not be a loopback address: other devices cannot dial it"
		case ip.IsUnspecified():
			return "must not be a wildcard address such as 0.0.0.0: no device can dial it"
		}
		return ""
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return "must not be localhost: other devices cannot dial it"
	}
	return ""
}

func checkPort(p string) string {
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "must use a port between 1 and 65535"
	}
	return ""
}
