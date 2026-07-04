// Package config resolves fleet's runtime directories and settings (~/.fleet).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const DefaultPort = 7433

type Config struct {
	Dir      string // ~/.fleet
	DBPath   string // ~/.fleet/fleet.db
	SpoolDir string // ~/.fleet/spool
	LogPath  string // ~/.fleet/hook.log
	Port     int
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
	if err := os.MkdirAll(c.SpoolDir, 0o755); err != nil {
		return nil, fmt.Errorf("create fleet dir: %w", err)
	}
	return c, nil
}

func (c *Config) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.Port)
}
