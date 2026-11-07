// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package config

import "testing"

func TestCheckLocalBaseURL(t *testing.T) {
	for base, ok := range map[string]bool{
		"":                        true,
		"osc://127.0.0.1:11000":   true,
		"http://127.0.0.1:4010":   true,
		"http://localhost:9":      true,
		"http://[::1]:8080":       true,
		"https://example.com":     false,
		"http://10.1.2.3":         false,
		"http://127.0.0.1.nip.io": false,
	} {
		if err := checkLocalBaseURL(base); (err == nil) != ok {
			t.Errorf("checkLocalBaseURL(%q) = %v, want ok=%v", base, err, ok)
		}
	}
}

func TestLoadRefusesRemoteBaseURL(t *testing.T) {
	t.Setenv("ABLETON_BASE_URL", "https://example.com")
	t.Setenv("ABLETON_CONFIG", t.TempDir()+"/none.toml")
	if _, err := Load(""); err == nil {
		t.Fatal("Load must refuse a remote ABLETON_BASE_URL")
	}
}
