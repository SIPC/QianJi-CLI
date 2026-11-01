// Command qianji is a CLI for the Qianji (钱迹) private API, driven by your own account.
//
// It signs requests exactly as the Android app does and talks to api.qianjiapp.com.
// Use it only with your own credentials. Read commands are safe; write commands ask
// before changing real data.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	cfg, err := loadConfig()
	if err != nil {
		fatal(err)
	}

	switch cmd {
	case "login":
		cmdLogin(cfg, args)
	case "logout":
		cmdLogout(cfg)
	case "whoami":
		cmdWhoami(cfg)
	case "books":
		cmdBooks(cfg)
	case "assets":
		cmdAssets(cfg)
	case "categories":
		cmdCategories(cfg, args)
	case "bills":
		cmdBills(cfg, args)
	case "raw":
		cmdRaw(cfg, args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `qianji — 钱迹 private API CLI (use your own account)

Usage:
  qianji login [--account <phone/email>]   Log in; password from $QIANJI_PASSWORD or prompt
  qianji whoami                             Show the logged-in user
  qianji logout                             Forget the saved token
  qianji books                              List account books (账本)
  qianji assets                             List assets (资产账户)
  qianji categories --book <id> [--type 0|1] List categories (支出/收入)
  qianji bills [--book <id>] [--pages N]    Pull bills (账单) and print JSON
  qianji raw <ctrl> <act> [key=value ...]   Signed call to any endpoint; prints JSON

Config is stored at ~/.qianji/config.json (token has file mode 0600).
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func requireLogin(cfg *Config) {
	if cfg.Token == "" || cfg.UID == "" {
		fatal(fmt.Errorf("not logged in; run `qianji login` first"))
	}
}

func printJSON(raw json.RawMessage) {
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		fmt.Println(string(raw))
		return
	}
	b, _ := json.MarshalIndent(pretty, "", "  ")
	fmt.Println(string(b))
}

// --- commands ---

func cmdLogin(cfg *Config, args []string) {
	account := cfg.Account
	for i := 0; i < len(args); i++ {
		if args[i] == "--account" && i+1 < len(args) {
			account = args[i+1]
			i++
		}
	}
	if account == "" {
		account = prompt("Account (phone/email): ")
	}
	pwd := os.Getenv("QIANJI_PASSWORD")
	if pwd == "" {
		pwd = prompt("Password (will echo; set $QIANJI_PASSWORD to avoid): ")
	}
	if account == "" || pwd == "" {
		fatal(fmt.Errorf("account and password are required"))
	}

	cfg.Account = account
	ensureDevID(cfg)
	c := newClient(cfg)
	env, err := c.call("account", "login", map[string]string{
		"v":   account,
		"pwd": md5hex(pwd),
	})
	if err != nil {
		fatal(err)
	}

	var data struct {
		Token string          `json:"token"`
		User  json.RawMessage `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		fatal(fmt.Errorf("parse login response: %w", err))
	}
	if data.Token == "" {
		fatal(fmt.Errorf("login succeeded but no token in response; raw data:\n%s", string(env.Data)))
	}
	cfg.Token = data.Token
	cfg.UID = extractUserID(data.User)
	cfg.Name = extractField(data.User, "name")
	if err := saveConfig(cfg); err != nil {
		fatal(err)
	}
	fmt.Printf("Logged in as %s (uid %s). Token saved to ~/.qianji/config.json\n", cfg.Name, cfg.UID)
}

func cmdLogout(cfg *Config) {
	cfg.Token = ""
	if err := saveConfig(cfg); err != nil {
		fatal(err)
	}
	fmt.Println("Logged out (token cleared).")
}

func cmdWhoami(cfg *Config) {
	if cfg.UID == "" {
		fmt.Println("Not logged in.")
		return
	}
	fmt.Printf("account: %s\nname:    %s\nuid:     %s\ntoken:   %s\n",
		cfg.Account, cfg.Name, cfg.UID, mask(cfg.Token))
}

func cmdBooks(cfg *Config) {
	requireLogin(cfg)
	env, err := newClient(cfg).call("book", "list", map[string]string{"t": "0"})
	if err != nil {
		fatal(err)
	}
	printJSON(env.Data)
}

func cmdAssets(cfg *Config) {
	requireLogin(cfg)
	env, err := newClient(cfg).call("asset", "list", map[string]string{"status": "1"})
	if err != nil {
		fatal(err)
	}
	printJSON(env.Data)
}

func cmdCategories(cfg *Config, args []string) {
	requireLogin(cfg)
	book, typ := "", "0"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--book":
			if i+1 < len(args) {
				book = args[i+1]
				i++
			}
		case "--type":
			if i+1 < len(args) {
				typ = args[i+1]
				i++
			}
		}
	}
	params := map[string]string{"t": typ}
	if book != "" {
		params["bookid"] = book
	}
	env, err := newClient(cfg).call("category", "listv2", params)
	if err != nil {
		fatal(err)
	}
	printJSON(env.Data)
}

func cmdBills(cfg *Config, args []string) {
	requireLogin(cfg)
	book, maxPages := "-1", 1
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--book":
			if i+1 < len(args) {
				book = args[i+1]
				i++
			}
		case "--pages":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &maxPages)
				i++
			}
		}
	}
	c := newClient(cfg)
	for page := 0; page < maxPages; page++ {
		env, err := c.call("syncv2", "pull", map[string]string{
			"bookid":     book,
			"pageoffset": itoa(int64(page)),
			"pagesign":   "0",
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("// page %d\n", page)
		printJSON(env.Data)
	}
}

func cmdRaw(cfg *Config, args []string) {
	if len(args) < 2 {
		fatal(fmt.Errorf("usage: qianji raw <ctrl> <act> [key=value ...]"))
	}
	ctrl, act := args[0], args[1]
	params := map[string]string{}
	for _, kv := range args[2:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			params[k] = v
		}
	}
	env, err := newClient(cfg).call(ctrl, act, params)
	if err != nil {
		fatal(err)
	}
	printJSON(env.Data)
}

// --- helpers ---

func prompt(label string) string {
	fmt.Fprint(os.Stderr, label)
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

func mask(s string) string {
	if len(s) <= 8 {
		return "********"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// extractUserID looks for the user id under the keys the app may use.
func extractUserID(user json.RawMessage) string {
	for _, k := range []string{"userid", "id", "uid"} {
		if v := extractField(user, k); v != "" {
			return v
		}
	}
	return ""
}

// extractField returns a string value from a JSON object, tolerating string or number.
func extractField(obj json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(obj, &m) != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return strings.Trim(string(raw), `"`)
}
