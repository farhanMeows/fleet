// Package config resolves fleet's runtime directories and settings (~/.fleet).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultPort = 7433

type Config struct {
	Dir      string // ~/.fleet
	DBPath   string // ~/.fleet/fleet.db
	SpoolDir string // ~/.fleet/spool
	LogPath  string // ~/.fleet/hook.log
	Port     int
	Bind     string // listen address; non-loopback requires the API token
	Token    string // contents of ~/.fleet/token, if present
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	dir := filepath.Join(home, ".fleet")
	c := &Config{
		Dir:      dir,
		DBPath:   filepath.Join(dir, "fleet.db"),
		SpoolDir: filepath.Join(dir, "spool"),
		LogPath:  filepath.Join(dir, "hook.log"),
		Port:     DefaultPort,
	}
	if v := os.Getenv("FLEET_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			c.Port = p
		}
	}
	c.Bind = "127.0.0.1"
	if v := os.Getenv("FLEET_BIND"); v != "" {
		c.Bind = v
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "token")); err == nil {
		c.Token = strings.TrimSpace(string(raw))
	}
	if err := os.MkdirAll(c.SpoolDir, 0o755); err != nil {
		return nil, fmt.Errorf("create fleet dir: %w", err)
	}
	return c, nil
}

func (c *Config) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.Port)
}
