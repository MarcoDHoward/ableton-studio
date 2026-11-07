// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "testing"

func TestDeliverAndFeedbackStayOnThisMachine(t *testing.T) {
	for _, ok := range []string{"webhook:http://127.0.0.1:8080/hook", "webhook:http://localhost/x", "webhook:http://[::1]:9/x", "file:out.json", "stdout"} {
		if _, err := ParseDeliverSink(ok); err != nil {
			t.Errorf("ParseDeliverSink(%q) = %v, want ok", ok, err)
		}
	}
	for _, bad := range []string{"webhook:https://example.com/hook", "webhook:http://10.0.0.5/x", "webhook:http://127.0.0.1.evil.example/x", "spacefast:page"} {
		if _, err := ParseDeliverSink(bad); err == nil {
			t.Errorf("ParseDeliverSink(%q) succeeded, want refusal", bad)
		}
	}
	if err := Deliver(DeliverSink{Scheme: "webhook", Target: "https://example.com/"}, []byte("{}"), false); err == nil {
		t.Error("Deliver to a remote webhook must be refused")
	}
	if err := postFeedback("https://feedback.example.com/", FeedbackEntry{}); err == nil {
		t.Error("feedback to a remote endpoint must be refused")
	}
}
