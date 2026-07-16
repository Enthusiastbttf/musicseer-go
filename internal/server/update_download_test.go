package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Verifies the security-critical path: fetchChecksum + downloadTo produce a
// hash that matches the published checksum, so handleUpdateApply's equality
// gate accepts a good download and (by construction) rejects a tampered one.
func TestDownloadAndChecksum(t *testing.T) {
	payload := []byte("fake musicseer binary contents \x00\x01\x02")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/musicseer-linux-amd64", func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  musicseer-linux-amd64\n", hexSum)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	want, err := fetchChecksum(ctx, srv.URL+"/checksums.txt", "musicseer-linux-amd64")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if want != hexSum {
		t.Fatalf("checksum parse: got %s want %s", want, hexSum)
	}

	dir := t.TempDir()
	path, got, err := downloadTo(ctx, dir, srv.URL+"/musicseer-linux-amd64")
	if err != nil {
		t.Fatalf("downloadTo: %v", err)
	}
	defer os.Remove(path)
	if got != want {
		t.Fatalf("downloaded checksum %s != published %s (tampered download would be rejected)", got, want)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(payload) {
		t.Fatal("downloaded content does not match served payload")
	}
}
