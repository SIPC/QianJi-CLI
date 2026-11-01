// Command qianji is the Qianji (钱迹) CLI, driven by your own account.
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
	args := parseOutputFlags(os.Args[2:])

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
	case "me", "profile":
		cmdMe(cfg)
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
	case "saving":
		cmdSimpleList(cfg, "saving", "list", nil)
	case "currency":
		cmdSimpleList(cfg, "currency", "listv2", nil)
	case "logins":
		cmdSimpleList(cfg, "account", "loginrecords2", renderLogins)
	case "repeat":
		cmdSimpleList(cfg, "repeat_task", "list", nil)
	case "installment":
		cmdSimpleList(cfg, "installment", "list", nil)
	case "billimage", "billimg":
		cmdBillImage(cfg, args)
	case "version", "--version", "-v":
		fmt.Println("qianji " + cliVersion)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `qianji %s — 钱迹 CLI (use your own account)

Usage:
  qianji login [--account <a>] [--password <p>]  Log in; password also via $QIANJI_PASSWORD or prompt
  qianji whoami                             Show the locally saved login (account/uid/token)
  qianji me                                 Show your online profile (name/email/VIP/avatar)
  qianji logout                             Forget the saved token
  qianji books                              List account books (账本)
  qianji assets                             List assets (资产账户)
  qianji categories --book <id> [--type 0|1] List categories (支出/收入)
  qianji bills [--book <id>] [--pages N]    Pull bills (账单) and print JSON
  qianji raw <ctrl> <act> [key=value ...]   Signed call to any endpoint; prints JSON
  qianji saving                             List saving plans (存钱计划)
  qianji currency                           List currencies (货币)
  qianji logins                             List login records / devices (登录记录)
  qianji repeat                             List repeat tasks (周期记账)
  qianji installment                        List installments (分期)
  qianji billimage <id>... [--size origin|md|small]  Bill image URL(s) from image ids
  qianji version                            Show CLI version

Output: human-readable by default; add --json for pretty JSON, --raw for the raw response.
Config is stored next to the executable in .qianji-cli/config.json (token has file mode 0600).
`, cliVersion)
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
	pwd := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--account":
			if i+1 < len(args) {
				account = args[i+1]
				i++
			}
		case "--password", "-p":
			if i+1 < len(args) {
				pwd = args[i+1]
				i++
			}
		}
	}
	if account == "" {
		account = prompt("Account (phone/email): ")
	}
	// Password precedence: --password flag, then $QIANJI_PASSWORD, then prompt.
	// A password passed on the command line can show up in shell history and the
	// process list, so prefer $QIANJI_PASSWORD on shared machines.
	if pwd == "" {
		pwd = os.Getenv("QIANJI_PASSWORD")
	}
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
	p, _ := configPath()
	fmt.Printf("Logged in as %s (uid %s). Token saved to %s\n", cfg.Name, cfg.UID, p)
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

// cmdMe prints the online user profile — name, email, gender, VIP, and the
// avatar URL. The app reads this from vip/config under config.userinfo.
func cmdMe(cfg *Config) {
	requireLogin(cfg)
	env, err := newClient(cfg).call("vip", "config", map[string]string{})
	if err != nil {
		fatal(err)
	}
	if outputMode == "raw" {
		os.Stdout.Write(env.Raw)
		fmt.Println()
		return
	}
	var wrap struct {
		Config struct {
			UserInfo json.RawMessage `json:"userinfo"`
		} `json:"config"`
	}
	if json.Unmarshal(env.Data, &wrap) != nil || len(wrap.Config.UserInfo) == 0 {
		printJSON(env.Data)
		return
	}
	if outputMode == "json" {
		printJSON(wrap.Config.UserInfo)
	} else {
		renderMe(wrap.Config.UserInfo)
	}
}

func cmdBooks(cfg *Config) {
	requireLogin(cfg)
	// t is a sync baseline (like lasttimes): 0 makes the server treat the client
	// as up to date and return nothing; -1 forces a full list of books.
	env, err := newClient(cfg).call("book", "list", map[string]string{"t": "-1"})
	if err != nil {
		fatal(err)
	}
	emit(env, renderBooks)
}

func cmdAssets(cfg *Config) {
	requireLogin(cfg)
	env, err := newClient(cfg).call("asset", "list", map[string]string{"status": "1"})
	if err != nil {
		fatal(err)
	}
	emit(env, renderAssets)
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
	emit(env, renderCategories)
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
	// syncv2/pull is cursor-paged: the first call uses pageoffset=0/pagesign=0,
	// and each response carries the next page's pageoffset + pagesign and a
	// hasmore flag. Follow the cursor rather than guessing offsets, and stop
	// early when hasmore=0. --pages caps how many pages to pull at most.
	pageoffset, pagesign := "0", "0"
	for page := 0; page < maxPages; page++ {
		env, err := c.call("syncv2", "pull", map[string]string{
			"bookid":     book,
			"pageoffset": pageoffset,
			"pagesign":   pagesign,
		})
		if err != nil {
			fatal(err)
		}
		emit(env, renderBills)

		var p struct {
			HasMore    int    `json:"hasmore"`
			PageOffset int64  `json:"pageoffset"`
			PageSign   string `json:"pagesign"`
		}
		if json.Unmarshal(env.Data, &p) != nil || p.HasMore == 0 {
			break
		}
		pageoffset, pagesign = itoa(p.PageOffset), p.PageSign
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
	emit(env, nil)
}

// cmdSimpleList calls a list endpoint that needs no extra params and prints its data.
func cmdSimpleList(cfg *Config, ctrl, act string, render func(json.RawMessage)) {
	requireLogin(cfg)
	env, err := newClient(cfg).call(ctrl, act, map[string]string{})
	if err != nil {
		fatal(err)
	}
	emit(env, render)
}

// cmdBillImage turns bill image ids (a bill's `images` field) into full URLs.
// Images are served from billImageHost as <id>!<spec>; spec picks the size
// (origin/md/small). No request is made here — the URLs are public by image id.
func cmdBillImage(_ *Config, args []string) {
	spec := "webporigin"
	var ids []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--size":
			if i+1 < len(args) {
				switch args[i+1] {
				case "origin":
					spec = "webporigin"
				case "md":
					spec = "webpmd640"
				case "small":
					spec = "webpsmall240"
				default:
					spec = args[i+1] // allow a raw spec
				}
				i++
			}
		default:
			ids = append(ids, args[i])
		}
	}
	if len(ids) == 0 {
		fatal(fmt.Errorf("usage: qianji billimage <image-id> [...] [--size origin|md|small]"))
	}
	for _, id := range ids {
		fmt.Printf("%s%s!%s\n", billImageHost, id, spec)
	}
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
