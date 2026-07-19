package setup

import "testing"

func TestParseWorkerDeployTag_Valid(t *testing.T) {
	tag, err := parseWorkerDeployTag([]byte(`{"result":{"tag":"v123"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "v123" {
		t.Errorf("got tag %q, want v123", tag)
	}
}

// A 2xx response whose body doesn't parse must surface an error rather than
// silently returning an empty tag as if the deploy succeeded.
func TestParseWorkerDeployTag_InvalidJSON(t *testing.T) {
	_, err := parseWorkerDeployTag([]byte(`<html>gateway error</html>`))
	if err == nil {
		t.Fatal("expected an error for an unparseable body, got nil")
	}
}
