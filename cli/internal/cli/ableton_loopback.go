// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

// Hand-written: ableton-pp-cli never sends data off this machine. The
// generated output sinks (--deliver webhook:) and the feedback endpoint are
// limited to loopback URLs; the generated Spacefast publishing sink is not
// shipped.

package cli

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// requireLoopbackURL refuses any URL whose host is not a loopback address
// (127.0.0.0/8, ::1) or the literal name "localhost".
func requireLoopbackURL(what, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s must be an http:// or https:// URL on this machine, got %q", what, raw)
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("%s must point at this machine (127.0.0.1, ::1 or localhost); ableton-pp-cli never sends data to other hosts, got host %q", what, host)
}
