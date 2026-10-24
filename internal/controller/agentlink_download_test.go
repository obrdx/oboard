package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/OboardProject/oboard/internal/agentlink"
)

func TestStealthDownloadReturnsOnlyRequestedChunks(t *testing.T) {
	root := t.TempDir()
	downloads := filepath.Join(root, "downloads")
	if err := os.MkdirAll(downloads, 0o700); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("signed-release"), 50000)
	if err := os.WriteFile(filepath.Join(downloads, "release-manifest.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{staticDir: filepath.Join(root, "web", "dist")}
	var got []byte
	for offset := 0; offset < len(content); offset += 512 << 10 {
		body, _ := json.Marshal(map[string]any{"stream": "release-manifest.json", "offset": offset, "length": 512 << 10})
		response := s.handleStealthDownloadRequest(&agentlink.RequestFrame{ID: int64(offset + 1), Body: body})
		if response.Status != 200 {
			t.Fatalf("offset %d status=%d error=%s", offset, response.Status, response.Error)
		}
		var chunk []byte
		if err := json.Unmarshal(response.Body, &chunk); err != nil {
			t.Fatal(err)
		}
		got = append(got, chunk...)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(content))
	}
	bad, _ := json.Marshal(map[string]any{"stream": "release-manifest.json", "offset": -1, "length": 1})
	if response := s.handleStealthDownloadRequest(&agentlink.RequestFrame{Body: bad}); response.Status != 400 {
		t.Fatalf("negative offset status=%d", response.Status)
	}
}

func TestAgentLinkDownloadsWindowsSignedArtifacts(t *testing.T) {
	root := t.TempDir()
	downloads := filepath.Join(root, "downloads")
	if err := os.MkdirAll(downloads, 0700); err != nil {
		t.Fatal(err)
	}
	server := &Server{staticDir: filepath.Join(root, "web", "dist")}
	for _, name := range []string{"oboard-agent-windows-amd64", "oboard-sb-windows-amd64", "oboard-realm-windows-amd64"} {
		if err := os.WriteFile(filepath.Join(downloads, name), []byte("signed-windows-artifact"), 0600); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"stream": name, "offset": 0, "length": 1024})
		response := server.handleStealthDownloadRequest(&agentlink.RequestFrame{Body: body})
		var chunk []byte
		if response.Status != 200 || json.Unmarshal(response.Body, &chunk) != nil || string(chunk) != "signed-windows-artifact" {
			t.Fatalf("Windows artifact %s failed: %+v", name, response)
		}
	}
}
