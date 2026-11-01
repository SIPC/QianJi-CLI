package main

import (
	"encoding/json"
	"fmt"
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

type catNode struct {
	Id      int64     `json:"id"`
	Name    string    `json:"name"`
	Sublist []catNode `json:"sublist"`
}

func collectCat(n catNode, m map[int64]string) {
	if n.Id != 0 {
		m[n.Id] = n.Name
	}
	for _, s := range n.Sublist {
		collectCat(s, m)
	}
}

func catCachePath(book string) (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "categories_"+sanitize(book)+".json"), nil
}

// loadCategoryMap returns a cateid -> name map for a book, cached locally. On a
// cache miss it pulls expense (t=0) and income (t=1) categories and saves them.
func loadCategoryMap(cfg *Config, book string) map[int64]string {
	if p, err := catCachePath(book); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			var m map[int64]string
			if json.Unmarshal(b, &m) == nil && len(m) > 0 {
				return m
			}
		}
	}
	m := map[int64]string{}
	c := newClient(cfg)
	for _, t := range []string{"0", "1"} {
		env, err := c.call("category", "listv2", map[string]string{"bookid": book, "t": t})
		if err != nil {
			continue
		}
		var w struct {
			List []catNode `json:"list"`
		}
		if json.Unmarshal(env.Data, &w) == nil {
			for _, n := range w.List {
				collectCat(n, m)
			}
		}
	}
	if len(m) > 0 {
		if p, err := catCachePath(book); err == nil {
			if b, e := json.Marshal(m); e == nil {
				_ = os.WriteFile(p, b, 0o600)
			}
		}
	}
	return m
}

// cateInfo is a category with its place in the tree; Parent is -1 at the top level.
type cateInfo struct {
	ID, Parent int64
	Name       string
}

// loadExpenseCategories pulls a book's expense categories uncached. Budget rules
// need the parent links and a current list, which the name cache above can't give.
func loadExpenseCategories(cfg *Config, book string) (map[int64]cateInfo, error) {
	env, err := newClient(cfg).call("category", "listv2", map[string]string{"bookid": book, "t": "0"})
	if err != nil {
		return nil, err
	}
	var w struct {
		List []catNode `json:"list"`
	}
	if err := json.Unmarshal(env.Data, &w); err != nil {
		return nil, fmt.Errorf("解析分类列表失败：%w", err)
	}
	if len(w.List) == 0 {
		return nil, fmt.Errorf("账本 %s 的支出分类为空", book)
	}
	m := map[int64]cateInfo{}
	for _, n := range w.List {
		m[n.Id] = cateInfo{ID: n.Id, Parent: -1, Name: n.Name}
		for _, s := range n.Sublist {
			m[s.Id] = cateInfo{ID: s.Id, Parent: n.Id, Name: s.Name}
		}
	}
	return m, nil
}

// syncBills runs an incremental pull into the local cache (via the server's
// lasttimes cursor) and returns every cached bill (raw JSON) plus how many
// changed this run. Shared by the CLI (cmdBillsSync) and the MCP server.
func syncBills(cfg *Config, book string, maxPages int) ([]json.RawMessage, int, error) {
	cache := loadBillsCache(book)
	c := newClient(cfg)
	lt := strings.TrimSpace(string(cache.LastTimes))
	pageoffset, pagesign := "0", "0"
	fetched := 0
	for page := 0; ; page++ {
		if maxPages > 0 && page >= maxPages {
			break
		}
		params := map[string]string{"bookid": book, "pageoffset": pageoffset, "pagesign": pagesign}
		if lt != "" && lt != "null" {
			params["lasttimes"] = lt
		}
		env, err := c.call("syncv2", "pull", params)
		if err != nil {
			if len(cache.Bills) > 0 {
				break // keep what we have on a mid-pull error
			}
			return nil, 0, err
		}
		var p struct {
			Changes   []json.RawMessage `json:"changes"`
			Deletes   []int64           `json:"deletes"`
			HasMore   int               `json:"hasmore"`
			PageOff   int64             `json:"pageoffset"`
			PageSign  string            `json:"pagesign"`
			LastTimes json.RawMessage   `json:"lasttimes"`
		}
		if json.Unmarshal(env.Data, &p) != nil {
			break
		}
		for _, ch := range p.Changes {
			var idw struct {
				ID json.Number `json:"id"`
			}
			if json.Unmarshal(ch, &idw) == nil && idw.ID != "" {
				cache.Bills[idw.ID.String()] = ch
			}
		}
		for _, d := range p.Deletes {
			delete(cache.Bills, itoa(d))
		}
		fetched += len(p.Changes)
		if len(p.LastTimes) > 0 && string(p.LastTimes) != "null" {
			cache.LastTimes = p.LastTimes
		}
		if p.HasMore == 0 {
			break
		}
		pageoffset, pagesign = itoa(p.PageOff), p.PageSign
	}
	_ = saveBillsCache(book, cache)
	out := make([]json.RawMessage, 0, len(cache.Bills))
	for _, ch := range cache.Bills {
		out = append(out, ch)
	}
	return out, fetched, nil
}
