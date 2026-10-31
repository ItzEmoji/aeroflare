package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func assetHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])[:32]
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectAssets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<h1>hi</h1>")
	writeFile(t, filepath.Join(dir, "css", "site.css"), "body{}")
	writeFile(t, filepath.Join(dir, ".DS_Store"), "junk")

	files, err := collectAssets(dir)
	if err != nil {
		t.Fatalf("collectAssets() error = %v", err)
	}

	got := map[string]assetFile{}
	for _, f := range files {
		got[f.Path] = f
	}
	if len(got) != 2 {
		t.Fatalf("collectAssets() = %v, want /index.html and /css/site.css only", got)
	}
	idx, ok := got["/index.html"]
	if !ok {
		t.Fatalf("missing /index.html in %v", got)
	}
	if idx.Hash != assetHash("<h1>hi</h1>") || idx.Size != int64(len("<h1>hi</h1>")) {
		t.Errorf("/index.html = hash %q size %d, want %q %d", idx.Hash, idx.Size, assetHash("<h1>hi</h1>"), len("<h1>hi</h1>"))
	}
	if _, ok := got["/css/site.css"]; !ok {
		t.Errorf("missing /css/site.css in %v", got)
	}
}

// No public/ directory (e.g. an older release) means no assets, not an error.
func TestCollectAssets_MissingDir(t *testing.T) {
	files, err := collectAssets(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(files) != 0 {
		t.Fatalf("collectAssets(missing) = %v, %v; want none, nil", files, err)
	}
}

// fakeAssetsAPI mimics Cloudflare's two-step asset upload: the session call
// returns an upload JWT and the buckets to send; the upload call returns the
// completion JWT once everything has arrived.
func fakeAssetsAPI(t *testing.T, buckets [][]string, uploaded map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts/acc/workers/scripts/w/assets-upload-session":
			if r.Header.Get("Authorization") != "Bearer cf-token" {
				t.Errorf("session auth = %q", r.Header.Get("Authorization"))
			}
			var body struct {
				Manifest map[string]struct {
					Hash string `json:"hash"`
					Size int64  `json:"size"`
				} `json:"manifest"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode manifest: %v", err)
			}
			if body.Manifest["/index.html"].Hash != assetHash("<h1>hi</h1>") {
				t.Errorf("manifest = %+v", body.Manifest)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result":  map[string]any{"jwt": "upload-jwt", "buckets": buckets},
			})
		case "/accounts/acc/workers/assets/upload":
			if r.Header.Get("Authorization") != "Bearer upload-jwt" {
				t.Errorf("upload auth = %q", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("base64") != "true" {
				t.Errorf("upload base64 query = %q", r.URL.RawQuery)
			}
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("read part: %v", err)
				}
				data, _ := io.ReadAll(p)
				dec, err := base64.StdEncoding.DecodeString(string(data))
				if err != nil {
					t.Errorf("part %q not base64: %v", p.FormName(), err)
				}
				uploaded[p.FormName()] = string(dec)
				if ct := p.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
					t.Errorf("part content type = %q", ct)
				}
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result":  map[string]any{"jwt": "completion-jwt"},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func useAPIBase(t *testing.T, base string) {
	t.Helper()
	old := cfAPIBase
	cfAPIBase = base
	t.Cleanup(func() { cfAPIBase = old })
}

func TestUploadWorkerAssets(t *testing.T) {
	files := []assetFile{{Path: "/index.html", Hash: assetHash("<h1>hi</h1>"), Size: 11, Data: []byte("<h1>hi</h1>")}}
	uploaded := map[string]string{}
	srv := fakeAssetsAPI(t, [][]string{{files[0].Hash}}, uploaded)
	useAPIBase(t, srv.URL)

	jwt, err := uploadWorkerAssets("acc", "cf-token", "w", files)
	if err != nil {
		t.Fatalf("uploadWorkerAssets() error = %v", err)
	}
	if jwt != "completion-jwt" {
		t.Errorf("jwt = %q, want completion-jwt", jwt)
	}
	if uploaded[files[0].Hash] != "<h1>hi</h1>" {
		t.Errorf("uploaded = %v", uploaded)
	}
}

// When Cloudflare already has every file, it returns no buckets and the
// session JWT doubles as the completion token.
func TestUploadWorkerAssets_AlreadyUploaded(t *testing.T) {
	files := []assetFile{{Path: "/index.html", Hash: assetHash("<h1>hi</h1>"), Size: 11, Data: []byte("<h1>hi</h1>")}}
	srv := fakeAssetsAPI(t, [][]string{}, map[string]string{})
	useAPIBase(t, srv.URL)

	jwt, err := uploadWorkerAssets("acc", "cf-token", "w", files)
	if err != nil || jwt != "upload-jwt" {
		t.Fatalf("uploadWorkerAssets() = %q, %v; want upload-jwt, nil", jwt, err)
	}
}

func TestUploadWorkerAssets_NoFiles(t *testing.T) {
	jwt, err := uploadWorkerAssets("acc", "cf-token", "w", nil)
	if err != nil || jwt != "" {
		t.Fatalf("uploadWorkerAssets(nil) = %q, %v; want \"\", nil", jwt, err)
	}
}

func TestWorkerUploadMetadata_Assets(t *testing.T) {
	with := workerUploadMetadata("2024-12-01", nil, "completion-jwt")
	assets, ok := with["assets"].(map[string]interface{})
	if !ok || assets["jwt"] != "completion-jwt" {
		t.Errorf("assets = %#v, want jwt completion-jwt", with["assets"])
	}
	if without := workerUploadMetadata("2024-12-01", nil, ""); without["assets"] != nil {
		t.Errorf("assets without jwt = %#v, want absent", without["assets"])
	}
}

func TestWorkerBundleFromTarball_Assets(t *testing.T) {
	tarball := buildTarball(t, map[string]string{
		"aeroflare-2.0.0/proxy/no-webui-native/worker.js":           "export default {}",
		"aeroflare-2.0.0/proxy/no-webui-native/public/index.html":   "<h1>hi</h1>",
		"aeroflare-2.0.0/proxy/no-webui-native/public/css/site.css": "body{}",
		"aeroflare-2.0.0/proxy/no-webui-native/public/../evil.txt":  "escape",
		"aeroflare-2.0.0/docs/public/other.html":                    "unrelated",
	})

	b, err := workerBundleFromTarball(bytes.NewReader(tarball))
	if err != nil {
		t.Fatalf("workerBundleFromTarball() error = %v", err)
	}
	if string(b.Script) != "export default {}" {
		t.Errorf("script = %q", b.Script)
	}
	want := map[string]string{"index.html": "<h1>hi</h1>", "css/site.css": "body{}"}
	if len(b.Assets) != len(want) {
		t.Fatalf("assets = %v, want %v", b.Assets, want)
	}
	for k, v := range want {
		if string(b.Assets[k]) != v {
			t.Errorf("assets[%q] = %q, want %q", k, b.Assets[k], v)
		}
	}
}
