// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import "context"

// sessionSnapshotKey marks a context as coming from 'snapshot save' or
// 'snapshot diff --live'. The key type is unexported, so no request
// parameter, MCP tool argument or spec field can set it: only Go code that
// calls WithSessionSnapshot.
type sessionSnapshotKey struct{}

// WithSessionSnapshot allows the GET /snapshot route (AbletonMCP
// get_session_snapshot) for requests made with the returned context.
// Only the snapshot save / snapshot diff --live commands call it.
func WithSessionSnapshot(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionSnapshotKey{}, true)
}

func sessionSnapshotAllowed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(sessionSnapshotKey{}).(bool)
	return v
}
