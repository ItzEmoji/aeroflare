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

// The proxy relies on Workers Cache (cached in front of the Worker, kept across
// redeploys) to honor its long Cache-Control TTLs. `init` deploys through the
// API rather than wrangler, so the upload metadata must enable it itself.
func TestWorkerUploadMetadata_EnablesWorkersCache(t *testing.T) {
	meta := workerUploadMetadata("2026-06-20", nil)

	cache, ok := meta["cache_options"].(map[string]interface{})
	if !ok {
		t.Fatalf("cache_options missing or wrong type: %#v", meta["cache_options"])
	}
	if cache["enabled"] != true {
		t.Errorf("cache_options.enabled = %v, want true", cache["enabled"])
	}
	if cache["cross_version_cache"] != true {
		t.Errorf("cache_options.cross_version_cache = %v, want true", cache["cross_version_cache"])
	}
	if meta["main_module"] != "worker.js" || meta["compatibility_date"] != "2026-06-20" {
		t.Errorf("unexpected base metadata: %#v", meta)
	}
}
