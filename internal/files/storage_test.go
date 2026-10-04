package files

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"

	"nemi/internal/config"
	"nemi/internal/vault"
)

func TestLocalAndS3StoreCiphertextWithoutPublicURLs(t *testing.T) {
	v, _ := vault.New(bytes.Repeat([]byte{19}, 32))
	cipher, err := v.Seal("owner:file", []byte("private source"))
	if err != nil {
		t.Fatal(err)
	}
	var object []byte
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/nemi-files/"+strings.Repeat("a", 64)+"/"+strings.Repeat("b", 32) {
			t.Error("unexpected object path")
		}
		if r.Method == "PUT" {
			var body io.Reader = r.Body
			if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
				body = httputil.NewChunkedReader(r.Body)
			}
			object, _ = io.ReadAll(body)
			w.Header().Set("ETag", `"fixture"`)
		} else {
			w.Header().Set("Last-Modified", "Mon, 05 Oct 2026 00:00:00 GMT")
			w.Header().Set("ETag", `"fixture"`)
			w.Write(object)
		}
	}))
	defer server.Close()
	for _, cfg := range []config.Config{{FilesRoot: t.TempDir()}, {FilesS3Endpoint: strings.TrimPrefix(server.URL, "http://"), FilesS3Bucket: "nemi-files", FilesS3Access: "fixture", FilesS3Secret: "fixture-secret", FilesS3Region: "us-east-1", FilesS3Secure: false}} {
		o, _, err := objects(cfg)
		if err != nil {
			t.Fatal(err)
		}
		key := strings.Repeat("a", 64) + "/" + strings.Repeat("b", 32)
		if err = o.Put(context.Background(), key, cipher); err != nil {
			t.Fatal(err)
		}
		got, err := o.Get(context.Background(), key)
		if err != nil || !bytes.Equal(got, cipher) || bytes.Contains(got, []byte("private source")) {
			t.Fatal("ciphertext roundtrip failed", err)
		}
		if _, err = v.Reveal("other:file", got); err == nil {
			t.Fatal("cross-owner decrypt succeeded")
		}
	}
	if requests != 2 {
		t.Fatal("storage retried", requests)
	}
	l := localObjects{t.TempDir()}
	if l.Put(context.Background(), "../escape", cipher) == nil {
		t.Fatal("unsafe object path accepted")
	}
}
