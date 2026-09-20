package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	SchemaVersion = 1
	DefaultURL    = "http://127.0.0.1:9090"
)

type File struct {
	SchemaVersion int    `json:"schema_version"`
	URL           string `json:"url,omitempty"`
	Token         string `json:"token,omitempty"`
}

type Overrides struct {
	URL      string
	URLSet   bool
	Token    string
	TokenSet bool
}

type Environment struct {
	URL      string
	URLSet   bool
	Token    string
	TokenSet bool
}

type Resolved struct {
	URL         string `json:"url"`
	URLSource   string `json:"url_source"`
	Token       string `json:"-"`
	TokenSource string `json:"token_source,omitempty"`
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(dir, "bediz", "config.json"), nil
}

func EnvironmentFrom(getenv func(string) (string, bool)) Environment {
	urlValue, urlSet := getenv("BEDIZ_URL")
	token, tokenSet := getenv("BEDIZ_TOKEN")
	return Environment{URL: urlValue, URLSet: urlSet, Token: token, TokenSet: tokenSet}
}

func Load(path string) (File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("open configuration: %w", err)
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var cfg File
	if err := decoder.Decode(&cfg); err != nil {
		return File{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return File{}, err
	}
	if cfg.SchemaVersion != SchemaVersion {
		return File{}, fmt.Errorf("unsupported configuration schema version %d", cfg.SchemaVersion)
	}
	if cfg.URL != "" {
		normalized, err := NormalizeURL(cfg.URL)
		if err != nil {
			return File{}, fmt.Errorf("configuration url: %w", err)
		}
		cfg.URL = normalized
	}
	return cfg, nil
}

func Save(path string, cfg File) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = SchemaVersion
	}
	if cfg.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported configuration schema version %d", cfg.SchemaVersion)
	}
	if cfg.URL != "" {
		normalized, err := NormalizeURL(cfg.URL)
		if err != nil {
			return err
		}
		cfg.URL = normalized
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(cfg); err != nil {
		temp.Close()
		return fmt.Errorf("write configuration: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close configuration: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}

func Resolve(overrides Overrides, env Environment, file File) (Resolved, error) {
	resolved := Resolved{URL: DefaultURL, URLSource: "default"}
	if file.URL != "" {
		resolved.URL = file.URL
		resolved.URLSource = "config"
	}
	if file.Token != "" {
		resolved.Token = file.Token
		resolved.TokenSource = "config"
	}
	if env.URLSet {
		resolved.URL = env.URL
		resolved.URLSource = "environment"
	}
	if env.TokenSet {
		resolved.Token = env.Token
		resolved.TokenSource = "environment"
	}
	if overrides.URLSet {
		resolved.URL = overrides.URL
		resolved.URLSource = "flag"
	}
	if overrides.TokenSet {
		resolved.Token = overrides.Token
		resolved.TokenSource = "flag"
	}

	normalized, err := NormalizeURL(resolved.URL)
	if err != nil {
		return Resolved{}, fmt.Errorf("%s url: %w", resolved.URLSource, err)
	}
	resolved.URL = normalized
	return resolved, nil
}

func NormalizeURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("url cannot be empty")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("url scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("url must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("url must not include credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("url must not include a query or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	return errors.New("decode configuration: multiple JSON values")
}
