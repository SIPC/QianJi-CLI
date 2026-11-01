package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"
)

// cmdMCP starts an HTTP MCP server. Auth is the secret path: requests must POST
// to /mcp/<secret>; anything else 404s. Set $QIANJI_MCP_PATH to pin the secret,
// otherwise a random one is generated and printed at startup.
func cmdMCP(_ *Config, args []string) {
	// Start without requiring login: bring the server up first, then `qianji
	// login` (e.g. via docker exec, writing to the mounted volume). Each tool
	// call reloads the config, so calls work as soon as you've logged in.
	addr := ":8787"
	for i := 0; i < len(args); i++ {
		if args[i] == "--addr" && i+1 < len(args) {
			addr = args[i+1]
			i++
		}
	}
	secret := os.Getenv("QIANJI_MCP_PATH")
	if secret == "" {
		var b [32]byte
		_, _ = rand.Read(b[:])
		secret = hex.EncodeToString(b[:])
	}
	path := "/mcp/" + secret
	mux := http.NewServeMux()
	mux.HandleFunc(path, mcpHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	fmt.Fprintf(os.Stderr, "qianji MCP server on %s\n  endpoint: POST http://<host>%s\n  tools:    %d（读 + 写）\n  固定路径设 $QIANJI_MCP_PATH；否则每次启动随机生成\n", addr, path, len(mcpTools))
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal(err)
	}
}

type mcpTool struct {
	Name, Desc string
	Schema     map[string]any
	Run        func(cfg *Config, a map[string]any) (string, error)
}

func mcpHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			serveSSE(w, r) // Streamable HTTP: open a server->client SSE stream
			return
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK) // session teardown (we're stateless)
			return
		case http.MethodPost:
			// handled below
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &req) != nil {
			rpcErr(w, nil, -32700, "parse error")
			return
		}
		switch req.Method {
		case "initialize":
			var ip struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &ip)
			pv := ip.ProtocolVersion // echo the client's version for compatibility
			if pv == "" {
				pv = "2025-06-18"
			}
			w.Header().Set("Mcp-Session-Id", newSessionID())
			rpcOK(w, req.ID, map[string]any{
				"protocolVersion": pv,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "qianji", "version": cliVersion},
			})
		case "ping":
			rpcOK(w, req.ID, map[string]any{})
		case "notifications/initialized", "notifications/cancelled":
			w.WriteHeader(http.StatusAccepted) // notifications take no response
		case "tools/list":
			tools := make([]map[string]any, 0, len(mcpTools))
			for _, t := range mcpTools {
				tools = append(tools, map[string]any{"name": t.Name, "description": t.Desc, "inputSchema": t.Schema})
			}
			rpcOK(w, req.ID, map[string]any{"tools": tools})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			cfg, _ := loadConfig() // reload each call so a fresh login takes effect
			if cfg.Token == "" || cfg.UID == "" {
				rpcOK(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "未登录：请先在容器内执行 qianji login（如 docker exec -e QIANJI_PASSWORD=... <容器> qianji login --account <账号>），登录态写入卷后即可调用。"}}, "isError": true})
				return
			}
			// Content tools return MCP content blocks directly (e.g. inline images)
			// rather than a single text string; check them before the text tools.
			if run, ok := contentTools[p.Name]; ok {
				content, err := run(cfg, p.Arguments)
				if err != nil {
					rpcOK(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}}, "isError": true})
					return
				}
				rpcOK(w, req.ID, map[string]any{"content": content})
				return
			}
			for i := range mcpTools {
				if mcpTools[i].Name != p.Name {
					continue
				}
				out, err := mcpTools[i].Run(cfg, p.Arguments)
				if err != nil {
					rpcOK(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}}, "isError": true})
					return
				}
				rpcOK(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": out}}})
				return
			}
			rpcErr(w, req.ID, -32602, "unknown tool: "+p.Name)
		default:
			rpcErr(w, req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func rpcOK(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func rpcErr(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// serveSSE opens a Streamable-HTTP server->client stream. We don't push
// server-initiated messages, so it just keeps the connection alive with pings
// until the client disconnects — enough for clients that open a GET stream.
func serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// --- argument + schema helpers ---

func aStr(a map[string]any, k, def string) string {
	if v, ok := a[k].(string); ok && v != "" {
		return v
	}
	return def
}

func aNum(a map[string]any, k string) (float64, bool) {
	switch v := a[k].(type) {
	case float64:
		return v, true
	case string:
		if f, e := strconv.ParseFloat(v, 64); e == nil {
			return f, true
		}
	}
	return 0, false
}

func strProp(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
func numProp(d string) map[string]any { return map[string]any{"type": "number", "description": d} }

func objSchema(props map[string]any, req ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(req) > 0 {
		s["required"] = req
	}
	return s
}

func callData(cfg *Config, ctrl, act string, params map[string]string) (string, error) {
	env, err := newClient(cfg).call(ctrl, act, params)
	if err != nil {
		return "", err
	}
	return string(env.Data), nil
}

var mcpTools = []mcpTool{
	{"qianji_me", "当前用户资料：昵称/邮箱/性别/VIP/头像", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		env, err := newClient(cfg).call("vip", "config", map[string]string{})
		if err != nil {
			return "", err
		}
		var w struct {
			Config struct {
				UserInfo json.RawMessage `json:"userinfo"`
			} `json:"config"`
		}
		if json.Unmarshal(env.Data, &w) == nil && len(w.Config.UserInfo) > 0 {
			return string(w.Config.UserInfo), nil
		}
		return string(env.Data), nil
	}},
	{"qianji_books", "账本列表", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "book", "list", map[string]string{"t": "-1"})
	}},
	{"qianji_assets", "资产账户列表（含余额）", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "asset", "list", map[string]string{"status": "1"})
	}},
	{"qianji_categories", "分类列表（拿 cateid 用于记账/筛选）", objSchema(map[string]any{
		"bookid": strProp("账本 id，默认 -1"),
		"type":   strProp("0 支出 / 1 收入，默认 0"),
	}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "category", "listv2", map[string]string{"bookid": aStr(a, "bookid", "-1"), "t": aStr(a, "type", "0")})
	}},
	{"qianji_bills", "账单查询，按时间/类型/分类id/金额/关键词筛选，返回账单 JSON 数组（最新在前）。账单的 images 字段是图片 id 数组，可传给 qianji_billimage 查看原图", objSchema(map[string]any{
		"bookid":  strProp("账本 id，默认 -1"),
		"since":   strProp("起始日期 2006-01-02"),
		"until":   strProp("结束日期（不含当天之后），2006-01-02"),
		"type":    numProp("类型 0 支出 / 1 收入 / 2 转账 …"),
		"cateid":  numProp("分类 id"),
		"min":     numProp("最小金额"),
		"max":     numProp("最大金额"),
		"keyword": strProp("搜备注 / 来源 / 账户"),
		"limit":   numProp("最多返回条数"),
	}), billsToolRun},
	{"qianji_saving", "存钱计划列表", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "saving", "list", map[string]string{})
	}},
	{"qianji_currency", "货币列表与汇率", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "currency", "listv2", map[string]string{})
	}},
	{"qianji_logins", "登录记录 / 设备", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "account", "loginrecords2", map[string]string{})
	}},
	{"qianji_repeat", "周期记账任务", objSchema(map[string]any{}), func(cfg *Config, a map[string]any) (string, error) {
		return callData(cfg, "repeat_task", "list", map[string]string{})
	}},
	{"qianji_add", "记一笔账单（直接写入真实账单，谨慎）", objSchema(map[string]any{
		"type":    numProp("0 支出 / 1 收入 / 2 转账 …"),
		"money":   numProp("金额，必须 > 0"),
		"cateid":  numProp("分类 id（见 qianji_categories）"),
		"assetid": numProp("资产 id（见 qianji_assets）"),
		"bookid":  numProp("账本 id，默认 -1"),
		"remark":  strProp("备注"),
		"time":    strProp("时间 2006-01-02 15:04，默认现在"),
	}, "type", "money", "cateid", "assetid"), addToolRun},
	{"qianji_del", "按 id 删除账单（直接删除真实账单，谨慎）", objSchema(map[string]any{
		"billid": numProp("账单 id"),
	}, "billid"), delToolRun},
	{"qianji_budget", "查询某账本某月或某年的预算（总预算和分类预算）", objSchema(map[string]any{
		"bookid": strProp("真实账本 id 的字符串（见 qianji_books），不支持 -1"),
		"month":  strProp("月份 2026-11，默认当前月"),
		"year":   strProp("年份 2026，查年度预算；与 month 二选一"),
	}, "bookid"), budgetToolRun},
	{"qianji_budget_set", "修改一项预算：传 total 改总预算，或传 cate + money 改分类预算（money 为 0 表示删除）。服务器按整期覆盖，默认只返回预览，confirm=true 才真正写入", objSchema(map[string]any{
		"bookid":  strProp("真实账本 id 的字符串（见 qianji_books），不支持 -1"),
		"month":   strProp("月份 2026-11，默认当前月"),
		"year":    strProp("年份 2026，改年度预算；与 month 二选一"),
		"total":   numProp("总预算金额"),
		"cate":    strProp("支出分类名或 id（见 qianji_categories）；名字重复时要用 id"),
		"money":   numProp("分类预算金额，0 表示删除"),
		"confirm": map[string]any{"type": "boolean", "description": "true 才写入，默认只预览"},
	}, "bookid"), budgetSetToolRun},
	{"qianji_raw", "对任意签名接口发请求（高级）", objSchema(map[string]any{
		"ctrl":   strProp("控制器，如 book"),
		"act":    strProp("动作，如 list"),
		"params": map[string]any{"type": "object", "description": "表单参数键值对"},
	}, "ctrl", "act"), rawToolRun},
	// qianji_billimage returns image content blocks (see contentTools), so its
	// Run is nil; it is listed here only so tools/list advertises it.
	{"qianji_billimage", "把账单的图片 id（bills 的 images 字段）转成原图 URL，并把图片内联返回以便直接查看", objSchema(map[string]any{
		"id":   strProp("单个图片 id"),
		"ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "多个图片 id"},
		"size": strProp("origin 原图 / md 中图640 / small 缩略240，默认 origin"),
	}), nil},
}

// contentTools return MCP content blocks (text + image) instead of a single
// text string. Keyed by tool name; checked before the text-only mcpTools.
var contentTools = map[string]func(cfg *Config, a map[string]any) ([]map[string]any, error){
	"qianji_billimage": billimageToolRun,
}

// billImageSpec maps the friendly size to the host's spec suffix.
func billImageSpec(size string) string {
	switch size {
	case "md":
		return "webpmd640"
	case "small":
		return "webpsmall240"
	case "", "origin":
		return "webporigin"
	default:
		return size // allow a raw spec
	}
}

// billimageToolRun turns bill image ids into both their public URL (text) and
// the image bytes inlined as an MCP image block, so a client can show the photo
// directly. The images are public by id, so no signing/token is needed.
func billimageToolRun(_ *Config, a map[string]any) ([]map[string]any, error) {
	var ids []string
	if s := aStr(a, "id", ""); s != "" {
		ids = append(ids, s)
	}
	if raw, ok := a["ids"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				ids = append(ids, s)
			}
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("需要 id 或 ids（账单 images 字段里的图片 id）")
	}
	spec := billImageSpec(aStr(a, "size", "origin"))
	var content []map[string]any
	for _, id := range ids {
		url := billImageHost + id + "!" + spec
		content = append(content, map[string]any{"type": "text", "text": url})
		data, mime, err := fetchBillImage(url)
		if err != nil {
			content = append(content, map[string]any{"type": "text", "text": "（图片抓取失败：" + err.Error() + "）"})
			continue
		}
		content = append(content, map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(data),
			"mimeType": mime,
		})
	}
	return content, nil
}

// fetchBillImage downloads a public bill image and returns its bytes + MIME type.
func fetchBillImage(url string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20)) // cap at 20 MiB
	if err != nil {
		return nil, "", err
	}
	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "image/webp" // host serves webp
	}
	return data, mime, nil
}

func billsToolRun(cfg *Config, a map[string]any) (string, error) {
	book := aStr(a, "bookid", "-1")
	raws, _, err := syncBills(cfg, book, 0)
	if err != nil {
		return "", err
	}
	f := &billFilter{typ: -1}
	if v, ok := aNum(a, "type"); ok {
		f.typ = int(v)
	}
	if v, ok := aNum(a, "cateid"); ok {
		f.cate = int64(v)
	}
	if v, ok := aNum(a, "min"); ok {
		f.min, f.hasMin = v, true
	}
	if v, ok := aNum(a, "max"); ok {
		f.max = v
	}
	if s := aStr(a, "since", ""); s != "" {
		if t, e := parseBillTime(s); e == nil {
			f.since = t.Unix()
		}
	}
	if s := aStr(a, "until", ""); s != "" {
		if t, e := parseBillTime(s); e == nil {
			if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
				t = t.Add(24 * time.Hour)
			}
			f.until = t.Unix()
		}
	}
	f.keyword = aStr(a, "keyword", "")
	if v, ok := aNum(a, "limit"); ok {
		f.limit = int(v)
	}
	type rawTimed struct {
		t   int64
		raw json.RawMessage
	}
	var kept []rawTimed
	for _, ch := range raws {
		var r billRow
		if json.Unmarshal(ch, &r) != nil {
			continue
		}
		if f.match(r) {
			kept = append(kept, rawTimed{r.Time, ch})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].t > kept[j].t })
	if f.limit > 0 && len(kept) > f.limit {
		kept = kept[:f.limit]
	}
	out := make([]json.RawMessage, len(kept))
	for i := range kept {
		out[i] = kept[i].raw
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func addToolRun(cfg *Config, a map[string]any) (string, error) {
	typ, ok1 := aNum(a, "type")
	moneyV, ok2 := aNum(a, "money")
	cate, ok3 := aNum(a, "cateid")
	asset, ok4 := aNum(a, "assetid")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return "", fmt.Errorf("type/money/cateid/assetid 为必填数字")
	}
	if moneyV <= 0 {
		return "", fmt.Errorf("money 必须 > 0")
	}
	book := int64(-1)
	if v, ok := aNum(a, "bookid"); ok {
		book = int64(v)
	}
	t := time.Now()
	if s := aStr(a, "time", ""); s != "" {
		if pt, e := parseBillTime(s); e == nil {
			t = pt
		}
	}
	bill, id := buildBillPayload(cfg, int(typ), moneyV, int64(cate), int64(asset), book, aStr(a, "remark", ""), t)
	data, err := submitBills(cfg, []any{bill}, []any{})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"id":%d,"result":%s}`, id, data), nil
}

func delToolRun(cfg *Config, a map[string]any) (string, error) {
	v, ok := aNum(a, "billid")
	if !ok {
		return "", fmt.Errorf("billid 为必填数字")
	}
	return submitBills(cfg, []any{}, []any{int64(v)})
}

func rawToolRun(cfg *Config, a map[string]any) (string, error) {
	ctrl := aStr(a, "ctrl", "")
	act := aStr(a, "act", "")
	if ctrl == "" || act == "" {
		return "", fmt.Errorf("ctrl/act 为必填")
	}
	params := map[string]string{}
	if pm, ok := a["params"].(map[string]any); ok {
		for k, val := range pm {
			params[k] = fmt.Sprint(val)
		}
	}
	return callData(cfg, ctrl, act, params)
}
