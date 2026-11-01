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
	case "me", "profile", "whoami":
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
	case "add":
		cmdAdd(cfg, args)
	case "del", "delete":
		cmdDel(cfg, args)
	case "saving":
		cmdSimpleList(cfg, "saving", "list", renderSaving)
	case "currency":
		cmdSimpleList(cfg, "currency", "listv2", renderCurrency)
	case "logins":
		cmdSimpleList(cfg, "account", "loginrecords2", renderLogins)
	case "repeat":
		cmdSimpleList(cfg, "repeat_task", "list", renderRepeat)
	case "installment":
		cmdSimpleList(cfg, "installment", "list", renderInstallment)
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
  qianji me                                 Your profile: name/email/VIP/avatar + local token
  qianji logout                             Forget the saved token
  qianji books                              List account books (账本)
  qianji assets                             List assets (资产账户)
  qianji categories --book <id> [--type 0|1] List categories (支出/收入)
  qianji bills [--book <id>] [--pages N] [--no-cache]  Bills newest-first; local incremental cache; --book filters 账本
  qianji raw <ctrl> <act> [key=value ...]   Signed call to any endpoint; prints JSON
  qianji add --type <t> --money <m> --cate <id> --asset <id> [--book <id>] [--remark <s>] [--time <ts>] [--confirm]
                                            Write one bill (dry-run unless --confirm)
  qianji del <billid>... [--confirm]        Delete bill(s) by id (dry-run unless --confirm)
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

// cmdMe prints the online user profile — name, email, gender, VIP, and the
// avatar URL (from vip/config.userinfo), plus the local token (merged whoami).
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
		if cfg.Token != "" {
			fmt.Printf("token: %s（本地）\n", mask(cfg.Token))
		}
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
	book, maxPages := "-1", 0 // maxPages 0 = pull every page until hasmore=0
	noCache := false
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
		case "--no-cache":
			noCache = true
		}
	}
	// Human mode keeps a local incremental cache so repeat runs only fetch what
	// changed. raw/json want the actual responses and --no-cache forces a full
	// pull, so those bypass the cache.
	if outputMode == "raw" || outputMode == "json" || noCache {
		cmdBillsDirect(cfg, book, maxPages)
		return
	}
	cmdBillsSync(cfg, book, maxPages)
}

// cmdBillsDirect pulls pages straight from the API. syncv2/pull is cursor-paged
// and returns bills in sync order (by update time/id), so to show the newest we
// pull every page (until hasmore=0, or --pages N) and sort. raw/json print each
// page's response; human accumulates and renders.
func cmdBillsDirect(cfg *Config, book string, maxPages int) {
	c := newClient(cfg)
	pageoffset, pagesign := "0", "0"
	var rows []billRow
	deletes := 0
	truncated := false
	for page := 0; ; page++ {
		if maxPages > 0 && page >= maxPages {
			truncated = true
			break
		}
		env, err := c.call("syncv2", "pull", map[string]string{
			"bookid": book, "pageoffset": pageoffset, "pagesign": pagesign,
		})
		if err != nil {
			if len(rows) > 0 {
				fmt.Fprintf(os.Stderr, "warning: stopped at page %d: %v\n", page, err)
				truncated = true
				break
			}
			fatal(err)
		}
		if outputMode == "raw" || outputMode == "json" {
			emit(env, nil)
		}
		var p struct {
			Changes    []billRow `json:"changes"`
			Deletes    []int64   `json:"deletes"`
			HasMore    int       `json:"hasmore"`
			PageOffset int64     `json:"pageoffset"`
			PageSign   string    `json:"pagesign"`
		}
		if json.Unmarshal(env.Data, &p) != nil {
			break
		}
		rows = append(rows, p.Changes...)
		deletes += len(p.Deletes)
		if p.HasMore == 0 {
			break
		}
		pageoffset, pagesign = itoa(p.PageOffset), p.PageSign
	}
	if outputMode != "raw" && outputMode != "json" {
		renderBillRows(rows, deletes, truncated)
	}
}

// cmdBillsSync keeps a local cache (.qianji-cli/bills_<book>.json) and, via the
// server's lasttimes cursor, pulls only what changed since last run. First run
// is a full pull that builds the cache; later runs usually fetch 0–few bills.
func cmdBillsSync(cfg *Config, book string, maxPages int) {
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
				fmt.Fprintf(os.Stderr, "warning: sync stopped at page %d: %v\n", page, err)
				break
			}
			fatal(err)
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
	if err := saveBillsCache(book, cache); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save cache: %v\n", err)
	}
	rows := make([]billRow, 0, len(cache.Bills))
	for _, ch := range cache.Bills {
		var r billRow
		if json.Unmarshal(ch, &r) == nil {
			rows = append(rows, r)
		}
	}
	renderBillRows(rows, 0, false)
	if fetched > 0 {
		fmt.Fprintf(os.Stderr, "（本次同步 %d 条变更）\n", fetched)
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
