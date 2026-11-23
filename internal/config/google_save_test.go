package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGoogleSavePreservesDeclarationAndEdits(t *testing.T) {
	cfg, err := loadGoogle(t, `"drive":{"kind":"google","service":"drive","oauth2":{"client_id":"id"}}`)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["drive"]
	p.DefaultProject = "folder"
	p.OAuth2.Scopes = []string{"custom-scope"}
	cfg.Profiles["drive"] = p
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	got := saved.Profiles["drive"]
	if got.Kind != "google" || got.URL != "" || got.Auth != "" || got.OAuth2.TokenURL != "" ||
		got.DefaultProject != "folder" || got.OAuth2.Scopes[0] != "custom-scope" {
		t.Fatalf("declaration or edits lost: %+v", got)
	}
	again, err := Load(cfg.path)
	if err != nil || again.Profiles["drive"].Auth != "oauth2" {
		t.Fatalf("reload failed: %v", err)
	}
	if cfg.Profiles["drive"].Kind != "api" {
		t.Fatal("Save changed in-memory runtime profile")
	}
}

// Save replaces the file instead of rewriting it in place: a hard link to the old file keeps the old content,
// so a process killed mid-write can never leave a half-written config.
func TestSaveReplacesTheFileAtomically(t *testing.T) {
	cfg, err := loadGoogle(t, `"drive":{"kind":"google","service":"drive","oauth2":{"client_id":"id"}}`)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatal(err)
	}
	link := cfg.path + ".old"
	if err := os.Link(cfg.path, link); err != nil {
		t.Skip("hard links are not available:", err)
	}
	cfg.Policy.AllowWrite = true
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if kept, _ := os.ReadFile(link); string(kept) != string(old) {
		t.Fatalf("the old file was rewritten in place:\n%s", kept)
	}
	if info, err := os.Stat(cfg.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v; want 0600", info.Mode().Perm(), err)
	}
	if left, _ := filepath.Glob(cfg.path + ".tmp-*"); len(left) > 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
