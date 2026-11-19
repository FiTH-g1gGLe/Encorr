package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"0.1.54", "0.1.53", true}, {"v0.1.54", "0.1.53", true}, {"0.1.53", "0.1.53", false}, {"0.1.9", "0.1.10", false}, {"0.2.0", "0.1.99", true}, {"0.1.53", "0.1.54", false}}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Fatalf("newerVersion(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func releaseArchive(t *testing.T, name string, prog []byte) []byte {
	var buf bytes.Buffer
	if strings.HasSuffix(name, ".zip") {
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("README.md")
		_, _ = w.Write([]byte("readme"))
		w, _ = zw.Create("encorr.exe")
		_, _ = w.Write(prog)
		_ = zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "./README.md", Mode: 0o644, Size: 6, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("readme"))
	_ = tw.WriteHeader(&tar.Header{Name: "./encorr", Mode: 0o755, Size: int64(len(prog)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(prog)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestDownloadAndSwap(t *testing.T) {
	name := assetName()
	archive := releaseArchive(t, name, []byte("NEW PROGRAM"))
	sum := sha256.Sum256(archive)
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n" + strings.Repeat("0", 64) + "  other.zip\n"
	badSums := strings.Repeat("a", 64) + "  " + name + "\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file":
			_, _ = w.Write(archive)
		case "/sums":
			_, _ = w.Write([]byte(sums))
		case "/badsums":
			_, _ = w.Write([]byte(badSums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = oldTransport }()
	releaseHosts["127.0.0.1"] = true
	defer delete(releaseHosts, "127.0.0.1")

	dir := t.TempDir()
	exe := filepath.Join(dir, "encorr")
	if err := os.WriteFile(exe, []byte("OLD PROGRAM"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := exePath
	SetExePath(exe)
	defer func() { exePath = oldExe }()

	rel := func(sumsPath string) *ghRelease {
		r := &ghRelease{TagName: "v9.9.9"}
		r.Assets = append(r.Assets,
			struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
				Size int64  `json:"size"`
			}{Name: name, URL: srv.URL + "/file"},
			struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
				Size int64  `json:"size"`
			}{Name: "SHA256SUMS.txt", URL: srv.URL + sumsPath})
		return r
	}
	a := &App{}
	ctx := context.Background()

	if _, err := a.downloadUpdate(ctx, rel("/badsums")); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a wrong checksum must stop the update: %v", err)
	}
	p, err := a.downloadUpdate(ctx, rel("/sums"))
	if err != nil {
		t.Fatal(err)
	}
	if err := swapExe(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW PROGRAM" {
		t.Fatalf("program not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD PROGRAM" {
		t.Fatalf("old program not kept: %q", b)
	}

	// Only GitHub's download hosts are trusted.
	evil := rel("/sums")
	evil.Assets[0].URL = "https://example.com/encorr.zip"
	if _, err := a.downloadUpdate(ctx, evil); err == nil || !strings.Contains(err.Error(), "Refusing") {
		t.Fatalf("an unknown download host must be refused: %v", err)
	}
	noSums := rel("/sums")
	noSums.Assets = noSums.Assets[:1]
	if _, err := a.downloadUpdate(ctx, noSums); err == nil {
		t.Fatal("a release without checksums must not install automatically")
	}
}
