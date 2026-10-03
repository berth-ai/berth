package broker

import (
	"regexp"
	"strings"
)

// Device is the one-time code and URL of a device-flow login.
type Device struct {
	Code string `json:"code"`
	URL  string `json:"url"`
}

var (
	// gh prints "one-time code: 1A2B-3C4D" or "One-time code (1A2B-3C4D)";
	// glab prints "one-time code: QPMCJSWK", with no dash.
	deviceCode = regexp.MustCompile(`(?i:\bcode\b)\W{0,4}([A-Z0-9]{4,8}(?:-[A-Z0-9]{4,8})?)\b`)
	deviceURL  = regexp.MustCompile(`https://[^\s"'<>]+`)
)

// ParseDevice finds the device code and verification URL that gh and glab
// print while they wait for the browser. It returns nil until both appear.
func ParseDevice(log string) *Device {
	code := deviceCode.FindStringSubmatch(log)
	if code == nil {
		return nil
	}
	for _, u := range deviceURL.FindAllString(log, -1) {
		if strings.Contains(u, "device") || strings.Contains(u, "user_code") {
			return &Device{Code: code[1], URL: strings.TrimRight(u, ".,;")}
		}
	}
	return nil
}
