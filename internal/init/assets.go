package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// cfAPIBase is the Cloudflare API root; a variable so tests can point it at a
// local server.
var cfAPIBase = "https://api.cloudflare.com/client/v4"

// workerAssetsDirName is the static-assets folder next to worker.js
// (proxy/no-webui-native/public). Its files are served by Cloudflare before the
// Worker runs; public/index.html is the cache's landing page.
const workerAssetsDirName = "public"

// assetFile is one static asset: its manifest path ("/index.html"), the
// 32-hex-character content hash Cloudflare dedupes uploads on, and its bytes.
type assetFile struct {
	Path string
	Hash string
	Size int64
	Data []byte
}

// collectAssets reads every regular, non-hidden file below dir. A missing dir
// is not an error: it means there are no assets (e.g. an older release), and
// the Worker's built-in landing page is used instead.
func collectAssets(dir string) ([]assetFile, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	var files []assetFile
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files = append(files, assetFile{
			Path: "/" + filepath.ToSlash(rel),
			Hash: hex.EncodeToString(sum[:])[:32],
			Size: int64(len(data)),
			Data: data,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read assets in %s: %w", dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// uploadWorkerAssets uploads files with Cloudflare's direct-upload flow and
// returns the completion token to put in the Worker upload's metadata.assets.
// With no files it returns "" and the Worker is deployed without assets.
//
//  1. POST the manifest to .../scripts/<name>/assets-upload-session; Cloudflare
//     answers with an upload JWT and the "buckets" of hashes it still needs.
//  2. POST each bucket, base64-encoded, to .../workers/assets/upload using that
//     JWT; the response to the last one carries the completion JWT.
//
// If Cloudflare already has every file, there are no buckets and the session
// JWT is itself the completion token.
func uploadWorkerAssets(accountID, apiToken, workerName string, files []assetFile) (string, error) {
	if len(files) == 0 {
		return "", nil
	}

	manifest := map[string]map[string]interface{}{}
	byHash := map[string]assetFile{}
	for _, f := range files {
		manifest[f.Path] = map[string]interface{}{"hash": f.Hash, "size": f.Size}
		byHash[f.Hash] = f
	}
	body, _ := json.Marshal(map[string]interface{}{"manifest": manifest})

	var session struct {
		JWT     string     `json:"jwt"`
		Buckets [][]string `json:"buckets"`
	}
	sessionURL := fmt.Sprintf("%s/accounts/%s/workers/scripts/%s/assets-upload-session", cfAPIBase, accountID, workerName)
	if err := cfDo("POST", sessionURL, apiToken, "application/json", bytes.NewReader(body), &session); err != nil {
		return "", fmt.Errorf("start asset upload: %w", err)
	}
	if session.JWT == "" {
		return "", fmt.Errorf("start asset upload: no upload token in response")
	}
	if len(session.Buckets) == 0 {
		return session.JWT, nil
	}

	completion := ""
	uploadURL := fmt.Sprintf("%s/accounts/%s/workers/assets/upload?base64=true", cfAPIBase, accountID)
	for _, bucket := range session.Buckets {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, hash := range bucket {
			f, ok := byHash[hash]
			if !ok {
				return "", fmt.Errorf("asset upload asked for unknown hash %s", hash)
			}
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, hash, hash))
			h.Set("Content-Type", assetContentType(f.Path))
			part, err := w.CreatePart(h)
			if err != nil {
				return "", err
			}
			_, _ = io.WriteString(part, base64.StdEncoding.EncodeToString(f.Data))
		}
		_ = w.Close()

		var res struct {
			JWT string `json:"jwt"`
		}
		if err := cfDo("POST", uploadURL, session.JWT, w.FormDataContentType(), &buf, &res); err != nil {
			return "", fmt.Errorf("upload assets: %w", err)
		}
		if res.JWT != "" {
			completion = res.JWT
		}
	}
	if completion == "" {
		return "", fmt.Errorf("upload assets: no completion token in response")
	}
	return completion, nil
}

// assetContentType is the Content-Type Cloudflare will serve the asset with.
func assetContentType(p string) string {
	if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// cfDo sends a Cloudflare API request and decodes the envelope's "result" into
// out, turning a non-2xx status or success=false into an error.
func cfDo(method, url, bearer, contentType string, body io.Reader, out interface{}) error {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", contentType)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare API HTTP %d: %s", resp.StatusCode, raw)
	}
	var env struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("parse cloudflare response: %w", err)
	}
	if !env.Success {
		return fmt.Errorf("cloudflare API error: %s", env.Errors)
	}
	return json.Unmarshal(env.Result, out)
}
