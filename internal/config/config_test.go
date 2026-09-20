package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	resolved, err := Resolve(
		Overrides{URL: "https://flag.example/", URLSet: true, Token: "flag-token", TokenSet: true},
		Environment{URL: "https://env.example", URLSet: true, Token: "env-token", TokenSet: true},
		File{SchemaVersion: 1, URL: "https://config.example", Token: "config-token"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.URL != "https://flag.example" || resolved.URLSource != "flag" {
		t.Fatalf("unexpected URL resolution: %#v", resolved)
	}
	if resolved.Token != "flag-token" || resolved.TokenSource != "flag" {
		t.Fatalf("unexpected token resolution: %#v", resolved)
	}
}

func TestResolveFallsBackToDefault(t *testing.T) {
	resolved, err := Resolve(Overrides{}, Environment{}, File{SchemaVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.URL != DefaultURL || resolved.URLSource != "default" {
		t.Fatalf("unexpected default: %#v", resolved)
	}
}

func TestSaveIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := File{SchemaVersion: 1, URL: "https://invoke.example/", Token: "secret"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("configuration mode = %o, want 600", got)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://invoke.example" || got.Token != want.Token {
		t.Fatalf("loaded configuration = %#v", got)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"tokne":"typo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestNormalizeURLRejectsCredentials(t *testing.T) {
	if _, err := NormalizeURL("https://user:pass@example.com"); err == nil {
		t.Fatal("expected embedded credentials to be rejected")
	}
}
