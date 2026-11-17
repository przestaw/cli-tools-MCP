package config

import (
	"encoding/json"
	"os"
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
