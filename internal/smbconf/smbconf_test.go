package smbconf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acdtrx/tiny-samba/internal/config"
)

func TestGenerate_Golden(t *testing.T) {
	cfg := &config.Config{
		Users: []string{"alice", "bob", "carol"},
		Shares: map[string]config.Share{
			"documents": {
				Path:    "/data/documents",
				Comment: "Shared documents",
				Access: map[string]string{
					"alice": "rw",
					"bob":   "ro",
				},
			},
			"media": {
				Path: "/data/media",
				Access: map[string]string{
					"alice": "ro",
					"carol": "rw",
				},
			},
			"public": {
				Path:    "/data/public",
				Comment: "Public read-only",
				Guest:   true,
			},
		},
	}
	cfg.Server.ApplyDefaults()

	got := Generate(cfg)

	goldenPath := filepath.Join("testdata", "golden.conf")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("smb.conf mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
