package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// cliVersion is this tool's own version, surfaced as the device name (devname)
// so it shows up in Qianji's 登录记录 as e.g. "QianJi-CLI/0.1.0". It is a var so
// release builds can inject the tag: -ldflags "-X main.cliVersion=v1.2.3".
var cliVersion = "0.1.0"

// Config is persisted in a .qianji-cli directory next to the executable after login.
type Config struct {
	Account string `json:"account"`
	UID     string `json:"uid"`
	Token   string `json:"token"`
	Name    string `json:"name"`
	DevID   string `json:"devid"`
	Host    string `json:"host,omitempty"`

	// VS/VSN override the app version sent as the vs/vsn headers. Leave empty
	// to use the built-in defaults; set them (or $QIANJI_VS / $QIANJI_VSN) if
	// the server starts rejecting the built-in version as too old.
	VS  string `json:"vs,omitempty"`
	VSN string `json:"vsn,omitempty"`

	// UA overrides the User-Agent header (default mimics Android's Dalvik UA).
	UA string `json:"ua,omitempty"`

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
		Name:     def(d.Name, cliVersion),
		OSVer:    def(d.OSVer, "33"),
		Market:   def(d.Market, "qianji-cli"),
		TZOffset: def(d.TZOffset, "28800"),
		Lang:     def(d.Lang, "zh"),
		Region:   def(d.Region, "CN"),
	}
}

// configPath returns the config location. By default a .qianji-cli directory
// next to the executable (portable, travels with the binary). QIANJI_CONFIG_DIR
// overrides the directory — used in Docker to point at a mounted volume so the
// config and the bill/category caches persist across container restarts.
func configPath() (string, error) {
	if d := os.Getenv("QIANJI_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "config.json"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), ".qianji-cli", "config.json"), nil
}

func loadConfig() (*Config, error) {
	c := &Config{}
	if p, err := configPath(); err == nil {
		b, rerr := os.ReadFile(p)
		if rerr == nil {
			if err := json.Unmarshal(b, c); err != nil {
				return nil, err
			}
		} else if !errors.Is(rerr, os.ErrNotExist) {
			return nil, rerr
		}
	}
	// Env overrides let a container inject the login without a config file:
	//   QIANJI_UID / QIANJI_TOKEN / QIANJI_HOST.
	if v := os.Getenv("QIANJI_UID"); v != "" {
		c.UID = v
	}
	if v := os.Getenv("QIANJI_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("QIANJI_HOST"); v != "" {
		c.Host = v
	}
	return c, nil
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
