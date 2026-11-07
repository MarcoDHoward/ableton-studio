// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"fmt"
	"net"
)

// SetAbletonBridgesForTest points the router at fake bridges (tests in other
// packages drive whole commands against fake AbletonOSC / AbletonMCP /
// AbletonPPExtras servers). Every address must be loopback; the returned
// func restores the production endpoints. Never called by the CLI itself.
func SetAbletonBridgesForTest(oscLocal, oscRemote, mcp, ppx string) (func(), error) {
	for _, a := range []string{oscLocal, oscRemote, mcp, ppx} {
		host, _, err := net.SplitHostPort(a)
		if err != nil || host != "127.0.0.1" {
			return nil, fmt.Errorf("test bridge address %q must be 127.0.0.1:<port>", a)
		}
	}
	oldL, oldR, oldM, oldP := abletonOSCLocal, abletonOSCRemote, abletonMCPAddr, abletonPPXAddr
	abletonOSCLocal, abletonOSCRemote, abletonMCPAddr, abletonPPXAddr = oscLocal, oscRemote, mcp, ppx
	return func() {
		abletonOSCLocal, abletonOSCRemote, abletonMCPAddr, abletonPPXAddr = oldL, oldR, oldM, oldP
	}, nil
}
