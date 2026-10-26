package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewFormatForUser(t *testing.T) {
	for _, tc := range []struct {
		format string
		user   *authUser
		want   string
	}{
		{"", nil, "avif"}, {"png", nil, "avif"}, {"", &authUser{}, "avif"}, {"png", &authUser{}, "png"},
	} {
		if got := previewRequestForUser(previewRequest{Format: tc.format}, tc.user).Format; got != tc.want {
			t.Fatalf("format %q user=%v: got %q want %q", tc.format, tc.user, got, tc.want)
		}
	}
}

func TestDirectPreviewImageReturnsPixelsWithoutImageStore(t *testing.T) {
	p := &previewer{}
	srv := &server{store: newTestStore(t), previewer: p}
	rec := httptest.NewRecorder()
	installPreviewAVIFStub(t, `printf '0000ftypavif' > "$output"`)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/default:Default/preview-image", strings.NewReader(`{"size":256,"seed":"123456","mapGen":{"seed":123456,"autoplace_settings":{"tile":{"treat_missing_as_default":false},"entity":{"treat_missing_as_default":false}}}}`))
	srv.handleProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/avif" || !bytes.Contains(rec.Body.Bytes(), []byte("ftypavif")) {
		t.Fatalf("type=%s body=%q", rec.Header().Get("Content-Type"), rec.Body.Bytes())
	}
	if len(p.images) != 0 {
		t.Fatal("direct tile response retained a transient URL")
	}
}

func TestDirectPreviewRejectsInvalidFormatAndExactGuest(t *testing.T) {
	srv := &server{store: newTestStore(t), previewer: &previewer{}}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"format":"bad"}`, http.StatusBadRequest},
		{`{"format":"jpeg"}`, http.StatusBadRequest},
		{`{"engine":"factorio"}`, http.StatusForbidden},
		{`{"planet":"invalid"}`, http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		srv.handleProfile(rec, httptest.NewRequest(http.MethodPost, "/api/profiles/default:Default/preview-image", strings.NewReader(tc.body)))
		if rec.Code != tc.status {
			t.Fatalf("%s: status=%d body=%s", tc.body, rec.Code, rec.Body.String())
		}
	}
}

func TestPreviewPNGBypassesAVIFAndPreservesPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fillOpaquePreviewPNGTestImage(img)
	p := &previewer{}
	encoded, err := p.encodePreviewImage(context.Background(), img, "png")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded.data))
	if err != nil {
		t.Fatal(err)
	}
	assertPreviewPNGPixelsEqual(t, img, decoded)
	if p.avif != nil {
		t.Fatal("PNG selection initialized the AVIF encoder")
	}
}

func TestPreviewAVIFCanceled(t *testing.T) {
	p := &previewer{}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.encodePreviewImage(ctx, img, "avif"); err != context.Canceled {
		t.Fatalf("canceled request error=%v", err)
	}
}

func TestPreviewAVIFRoundTripAndCache(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed")
	}
	encoders, _ := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if !bytes.Contains(encoders, []byte("libaom-av1")) {
		t.Skip("FFmpeg lacks libaom-av1")
	}
	_, _, _, settings := benchmarkPreviewSettings()
	img, _, err := renderFastMapPreview(context.Background(), settings, 512, previewZoom{tilesPerPixel: 1, renderSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	// All eight cold requests must encode AVIF, including when encodes overlap.
	pConcurrent := &previewer{}
	start := make(chan struct{})
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			encoded, err := pConcurrent.encodePreviewImage(context.Background(), img, "avif")
			if err != nil {
				results <- err.Error()
				return
			}
			results <- encoded.contentType
		}()
	}
	close(start)
	for i := 0; i < 8; i++ {
		if got := <-results; got != "image/avif" {
			t.Errorf("concurrent encode returned %s, want image/avif", got)
		}
	}
	p := &previewer{}
	started := time.Now()
	encoded, err := p.encodePreviewImage(context.Background(), img, "avif")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.contentType != "image/avif" {
		t.Fatalf("AVIF encoder fell back: %s", encoded.contentType)
	}
	path := filepath.Join(t.TempDir(), "tile.avif")
	if err := os.WriteFile(path, encoded.data, 0600); err != nil {
		t.Fatal(err)
	}
	decoded, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-threads", "1", "-filter_threads", "1", "-i", path, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(img.Pix) {
		t.Fatalf("decoded bytes=%d want=%d", len(decoded), len(img.Pix))
	}
	pngData, err := encodeOpaquePreviewPNG(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded.data) >= len(pngData) {
		t.Fatalf("AVIF=%d PNG=%d", len(encoded.data), len(pngData))
	}
	repeat, err := p.encodePreviewImage(context.Background(), img, "avif")
	if err != nil || !bytes.Equal(repeat.data, encoded.data) {
		t.Fatal("cached tile did not preserve encoded bytes")
	}
	t.Logf("512px PNG=%d B AVIF=%d B encode=%s", len(pngData), len(encoded.data), elapsed)
}

func TestPreviewAVIFCacheIsBounded(t *testing.T) {
	c := (&previewer{}).avifEncoder()
	for i := 0; i < 40; i++ {
		var key [32]byte
		key[0] = byte(i)
		c.store(key, previewEncodedImage{data: make([]byte, 1<<20)})
	}
	if c.bytes > previewAVIFCacheBytes || len(c.entries) != 32 {
		t.Fatalf("cache bytes=%d entries=%d", c.bytes, len(c.entries))
	}
	if _, ok := c.get([32]byte{0}); ok {
		t.Fatal("oldest entry not evicted")
	}
}

func TestSignedInDirectPreviewCanSelectPNG(t *testing.T) {
	auth, password := newTestAuthStore(t)
	user, err := auth.authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.createSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv := &server{store: newTestStore(t), previewer: &previewer{}, auth: auth}
	for _, format := range []string{"png"} {
		req := httptest.NewRequest(http.MethodPost, "/api/profiles/default:Default/preview-image", strings.NewReader(`{"format":"`+format+`","size":256,"seed":"123456"}`))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		srv.handleProfile(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/"+format {
			t.Fatalf("%s status=%d type=%s", format, rec.Code, rec.Header().Get("Content-Type"))
		}
		decoded, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil || decoded.Bounds().Dx() != 256 {
			t.Fatalf("%s invalid image: %v", format, err)
		}
	}
}

// The stub consumes raw pixels and writes to FFmpeg's final output argument.
func installPreviewAVIFStub(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nfor output do :; done\n/bin/cat > /dev/null\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestPreviewAVIFDoesNotSwitchFormats(t *testing.T) {
	// A flat tile has a tiny PNG representation but must still use AVIF.
	installPreviewAVIFStub(t, `printf '0000ftypavif' > "$output"`)
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	encoded, err := (&previewer{}).encodePreviewImage(context.Background(), img, "avif")
	if err != nil || encoded.contentType != "image/avif" {
		t.Fatalf("flat tile type=%s err=%v", encoded.contentType, err)
	}
}

func TestPreviewAVIFFailureReturnsErrorWithoutFallback(t *testing.T) {
	installPreviewAVIFStub(t, "exit 1")
	p := &previewer{}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	encoded, err := p.encodePreviewImage(context.Background(), img, "avif")
	if err == nil || len(encoded.data) != 0 || encoded.contentType != "" {
		t.Fatalf("encoder failure returned type=%s bytes=%d err=%v", encoded.contentType, len(encoded.data), err)
	}
	if _, cached := p.avifEncoder().get(previewImagePixelKey(img)); cached {
		t.Fatal("failed encode was cached")
	}
}
