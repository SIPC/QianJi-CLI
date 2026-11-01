package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// billsCache is a local store of synced bills, next to the config. It lets
// `bills` pull only what changed since last run using the server's lasttimes
// cursor, instead of re-pulling every page each time.
type billsCache struct {
	LastTimes json.RawMessage            `json:"lasttimes,omitempty"`
	Bills     map[string]json.RawMessage `json:"bills"`
}

// sanitize keeps a book id safe to use in a filename.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func billsCachePath(book string) (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "bills_"+sanitize(book)+".json"), nil
}

func loadBillsCache(book string) *billsCache {
	c := &billsCache{Bills: map[string]json.RawMessage{}}
	p, err := billsCachePath(book)
	if err != nil {
		return c
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, c)
	if c.Bills == nil {
		c.Bills = map[string]json.RawMessage{}
	}
	return c
}

func saveBillsCache(book string, c *billsCache) error {
	p, err := billsCachePath(book)
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
