package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is persisted at ~/.qianji/config.json after login.
type Config struct {
	Account string `json:"account"`
	UID     string `json:"uid"`
	Token   string `json:"token"`
	Name    string `json:"name"`
	DevID   string `json:"devid"`
	Host    string `json:"host,omitempty"`

	// Device is the device fingerprint sent on every request. The server records
	// it as a login device (see "登录记录" in the app). Defaults identify this tool
	// as "QianJi-CLI"; override to match your real phone if you'd rather blend in.
	Device Device `json:"device,omitempty"`
}

// Device holds the device/locale headers. Empty fields fall back to defaults.
type Device struct {
	Brand    string `json:"brand,omitempty"`    // devbrand, e.g. "XIAOMI"
	Name     string `json:"name,omitempty"`     // devname, e.g. "M2012K11AC"
	OSVer    string `json:"osver,omitempty"`    // osvs, Android SDK int, e.g. "33"
	Market   string `json:"market,omitempty"`   // mk, app channel, e.g. "guanwang"
	TZOffset string `json:"tzoffset,omitempty"` // timezoneoffset in seconds, e.g. "28800"
	Lang     string `json:"lang,omitempty"`     // clang, e.g. "zh"
	Region   string `json:"region,omitempty"`   // cregion, e.g. "CN"
}

// withDefaults returns a copy with any empty field filled by a sensible default.
func (d Device) withDefaults() Device {
	def := func(v, fallback string) string {
		if v == "" {
			return fallback
		}
		return v
	}
	// Defaults identify this tool honestly rather than impersonating a real phone,
	// so it is recognizable in the account's 登录记录. Override to blend in if wanted.
	return Device{
		Brand:    def(d.Brand, "QianJi-CLI"),
		Name:     def(d.Name, "qianji-cli"),
		OSVer:    def(d.OSVer, "33"),
		Market:   def(d.Market, "qianji-cli"),
		TZOffset: def(d.TZOffset, "28800"),
		Lang:     def(d.Lang, "zh"),
		Region:   def(d.Region, "CN"),
	}
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".qianji", "config.json"), nil
}

func loadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveConfig(c *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// ensureDevID returns a stable per-install device id (md5-length hex), creating one if absent.
func ensureDevID(c *Config) string {
	if c.DevID == "" {
		var b [16]byte
		_, _ = rand.Read(b[:])
		c.DevID = hex.EncodeToString(b[:])
	}
	return c.DevID
}
