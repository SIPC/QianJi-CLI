package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	budgetFlagFull     = 1 // Budget.FLAG_FULL: the book's total budget
	budgetFlagCategory = 2 // Budget.FLAG_CATEGORY
	budgetTypeMonth    = 1
	budgetTypeYear     = 2
)

// budgetItem is one budget as the server sent it. It stays a map (decoded with
// UseNumber) so a read-modify-write keeps fields we don't model and large ids
// intact. JSON names come from the @SerializedName annotations on the app's
// Budget class: bookid, budgetid, cateid, typeinfo, notbudget, ...
type budgetItem map[string]any

// budgetPeriod is a budget's month or, with month 0, its whole year.
type budgetPeriod struct {
	year, month int
}

func (p budgetPeriod) budgetType() int {
	if p.month == 0 {
		return budgetTypeYear
	}
	return budgetTypeMonth
}

// typeInfo mirrors DateFilter.getValue. The month is NOT zero-padded: the app
// sends "2026,3", and a padded value would address a different period.
func (p budgetPeriod) typeInfo() string {
	if p.month == 0 {
		return strconv.Itoa(p.year)
	}
	return fmt.Sprintf("%d,%d", p.year, p.month)
}

func (p budgetPeriod) flts() string {
	key := "month"
	if p.month == 0 {
		key = "year"
	}
	b, _ := json.Marshal(map[string]string{key: p.typeInfo()})
	return string(b)
}

func (p budgetPeriod) String() string {
	if p.month == 0 {
		return fmt.Sprintf("%d 年", p.year)
	}
	return fmt.Sprintf("%d-%02d", p.year, p.month)
}

// resolveBudgetPeriod reads --month/--year, defaulting to the current month. For
// a book with a custom month start ("m<day>"), days before that start still
// belong to the previous month, as in BookConfig.makeSureCurrentMonth.
func resolveBudgetPeriod(month, year, rng string, now time.Time) (budgetPeriod, error) {
	if month != "" && year != "" {
		return budgetPeriod{}, fmt.Errorf("--month 和 --year 只能选一个")
	}
	if month != "" {
		ys, ms, ok := strings.Cut(month, "-")
		y, e1 := strconv.Atoi(ys)
		m, e2 := strconv.Atoi(ms)
		if !ok || e1 != nil || e2 != nil || y < 1970 || y > 9999 || m < 1 || m > 12 {
			return budgetPeriod{}, fmt.Errorf("--month 格式应为 2026-11，收到 %q", month)
		}
		return budgetPeriod{y, m}, nil
	}
	if year != "" {
		y, err := strconv.Atoi(year)
		if err != nil || y < 1970 || y > 9999 {
			return budgetPeriod{}, fmt.Errorf("--year 格式应为 2026，收到 %q", year)
		}
		return budgetPeriod{year: y}, nil
	}
	y, mon, d := now.Date()
	m := int(mon)
	if strings.HasPrefix(rng, "m") {
		if start, err := strconv.Atoi(rng[1:]); err == nil && d < start {
			if m--; m == 0 {
				y, m = y-1, 12
			}
		}
	}
	return budgetPeriod{y, m}, nil
}

// --- field access ---

func budgetNum(b budgetItem, key string) (float64, bool) {
	v, ok := b[key]
	if !ok || v == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	return f, err == nil
}

func budgetInt(b budgetItem, key string, def int64) int64 {
	v, ok := b[key]
	if !ok || v == nil {
		return def
	}
	s := fmt.Sprint(v)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	return def
}

func budgetMoney(b budgetItem) float64 {
	f, _ := budgetNum(b, "money")
	return f
}

// The app's Budget defaults: flag 2 (category) and cateid -1 when absent.
func isFullBudget(b budgetItem) bool {
	return budgetInt(b, "flag", budgetFlagCategory) == budgetFlagFull
}

func isCategoryBudget(b budgetItem) bool {
	return budgetInt(b, "flag", budgetFlagCategory) == budgetFlagCategory
}

func budgetCateID(b budgetItem) int64 { return budgetInt(b, "cateid", -1) }

func decodeBudgets(data json.RawMessage) ([]budgetItem, error) {
	var w struct {
		List *[]budgetItem `json:"list"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("解析预算列表失败：%w", err)
	}
	// A missing list must not read as "no budgets": submit overwrites the whole
	// period, so that mistake would wipe it.
	if w.List == nil {
		return nil, fmt.Errorf("预算接口的返回里没有 list 字段")
	}
	items := make([]budgetItem, 0, len(*w.List))
	for _, it := range *w.List {
		if it != nil {
			items = append(items, it)
		}
	}
	return items, nil
}

func cloneBudgets(items []budgetItem) []budgetItem {
	out := make([]budgetItem, len(items))
	for i, it := range items {
		c := make(budgetItem, len(it))
		for k, v := range it {
			c[k] = v
		}
		out[i] = c
	}
	return out
}

// --- limit rules (ported from the app's BudgetSetNew) ---

// budgetUsedAndLimit totals top-level category budgets plus sub-category budgets
// whose parent has none, so nothing is counted twice.
func budgetUsedAndLimit(items []budgetItem, cats map[int64]cateInfo) (used, limit float64) {
	parents := map[int64]bool{}
	add := func(b budgetItem) {
		limit += budgetMoney(b)
		u, _ := budgetNum(b, "used")
		used += u
	}
	for _, b := range items {
		if info, ok := cats[budgetCateID(b)]; ok && isCategoryBudget(b) && info.Parent == -1 {
			parents[info.ID] = true
			add(b)
		}
	}
	for _, b := range items {
		if info, ok := cats[budgetCateID(b)]; ok && isCategoryBudget(b) && info.Parent != -1 && !parents[info.Parent] {
			add(b)
		}
	}
	return used, limit
}

// normalizeBudgets is BudgetSetNew.__setup: it orders the list (total, then each
// parent followed by its subs, then orphaned subs) and raises limits so a parent
// covers its subs and the total covers all categories. newFull supplies the total
// when categories exist without one.
//
// Unlike the app, which silently drops budgets whose category it can't find, these
// stay at the end of the list untouched: losing one to a stale category list is
// worse than sending it back.
func normalizeBudgets(items []budgetItem, cats map[int64]cateInfo, newFull func() budgetItem) []budgetItem {
	type group struct {
		parent budgetItem
		subs   []budgetItem
	}
	var full budgetItem
	var groups []*group
	byParent := map[int64]*group{}
	var orphans, unknown []budgetItem

	for _, b := range items {
		if isFullBudget(b) {
			full = b
			continue
		}
		info, ok := cats[budgetCateID(b)]
		if !ok || !isCategoryBudget(b) {
			unknown = append(unknown, b)
			continue
		}
		if info.Parent == -1 {
			if g := byParent[info.ID]; g != nil {
				g.parent = b
			} else {
				g = &group{parent: b}
				byParent[info.ID] = g
				groups = append(groups, g)
			}
		}
	}
	for _, b := range items {
		info, ok := cats[budgetCateID(b)]
		if !ok || !isCategoryBudget(b) || info.Parent == -1 {
			continue
		}
		if g := byParent[info.Parent]; g != nil {
			g.subs = append(g.subs, b)
		} else {
			orphans = append(orphans, b)
		}
	}

	if full == nil && (len(groups) > 0 || len(orphans) > 0) {
		full = newFull()
	}
	out := make([]budgetItem, 0, len(items)+1)
	if full != nil {
		out = append(out, full)
	}
	for _, g := range groups {
		sum := 0.0
		for _, s := range g.subs {
			sum += budgetMoney(s)
		}
		if sum > budgetMoney(g.parent) {
			g.parent["money"] = sum
		}
		out = append(out, g.parent)
		out = append(out, g.subs...)
	}
	out = append(out, orphans...)
	if full != nil {
		used, limit := budgetUsedAndLimit(out, cats)
		if limit > budgetMoney(full) {
			full["money"] = limit
		}
		full["totalCateUsed"] = used
		full["totalCateLimit"] = limit
	}
	return append(out, unknown...)
}

// --- changes ---

// budgetContext holds what a new budget needs (Budget.newForFull/newForCategory).
type budgetContext struct {
	period budgetPeriod
	book   int64
	uid    string
}

func (c budgetContext) newBudget(flag int, cate int64) budgetItem {
	return budgetItem{
		"bookid":   c.book,
		"userid":   c.uid,
		"type":     c.period.budgetType(),
		"typeinfo": c.period.typeInfo(),
		"flag":     flag,
		"cateid":   cate,
		"money":    0.0,
	}
}

func (c budgetContext) newFull() budgetItem { return c.newBudget(budgetFlagFull, -1) }

// budgetChange is one edit: the total limit, or a category limit (<= 0 removes it).
type budgetChange struct {
	total *float64
	cate  int64
	money float64
}

// applyBudgetChange edits items in place and returns the normalized list.
func applyBudgetChange(items []budgetItem, ch budgetChange, cats map[int64]cateInfo, ctx budgetContext) ([]budgetItem, error) {
	if ch.total != nil {
		// As in the app, the total can't be set below the category limits.
		if _, limit := budgetUsedAndLimit(items, cats); *ch.total < limit {
			return nil, fmt.Errorf("总预算 %s 不能低于分类预算合计 %s", money(*ch.total), money(limit))
		}
		var full budgetItem
		for _, b := range items {
			if isFullBudget(b) {
				full = b
			}
		}
		if full == nil {
			full = ctx.newFull()
			items = append(items, full)
		}
		full["money"] = *ch.total
		return normalizeBudgets(items, cats, ctx.newFull), nil
	}

	idx := -1
	for i, b := range items {
		if isCategoryBudget(b) && budgetCateID(b) == ch.cate {
			idx = i
		}
	}
	switch {
	case ch.money <= 0 && idx < 0:
		return nil, fmt.Errorf("分类 %d 当期没有设预算，无需删除", ch.cate)
	case ch.money <= 0:
		items = append(items[:idx], items[idx+1:]...)
	case idx >= 0:
		items[idx]["money"] = ch.money
	default:
		if _, ok := cats[ch.cate]; !ok {
			return nil, fmt.Errorf("找不到支出分类 %d", ch.cate)
		}
		b := ctx.newBudget(budgetFlagCategory, ch.cate)
		b["money"] = ch.money
		items = append(items, b)
	}
	return normalizeBudgets(items, cats, ctx.newFull), nil
}

// resolveBudgetCate turns --cate into an id. Unlike the bills filter, a repeated
// name is an error: a write must never pick one at random. An id may also be one
// only present in the budget list, so a budget on a deleted category can be removed.
func resolveBudgetCate(arg string, cats map[int64]cateInfo, items []budgetItem) (int64, error) {
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		if _, ok := cats[id]; ok {
			return id, nil
		}
		for _, b := range items {
			if isCategoryBudget(b) && budgetCateID(b) == id {
				return id, nil
			}
		}
		return 0, fmt.Errorf("找不到支出分类 id %d（用 qianji categories --book <id> 查）", id)
	}
	var hits []int64
	for id, c := range cats {
		if c.Name == arg {
			hits = append(hits, id)
		}
	}
	switch len(hits) {
	case 0:
		return 0, fmt.Errorf("找不到支出分类 %q（用 qianji categories --book <id> 查）", arg)
	case 1:
		return hits[0], nil
	}
	return 0, fmt.Errorf("分类名 %q 重复（id %v），请改用 id", arg, hits)
}

func budgetLabel(b budgetItem, cats map[int64]cateInfo) string {
	if isFullBudget(b) {
		return "总预算"
	}
	id := budgetCateID(b)
	if c, ok := cats[id]; ok {
		return c.Name
	}
	return fmt.Sprintf("分类 %d（已不存在）", id)
}

// budgetDiff describes every limit that differs, including ones raised by the
// rules rather than asked for, so a preview shows the write's real effect.
func budgetDiff(before, after []budgetItem, cats map[int64]cateInfo) []string {
	key := func(b budgetItem) string {
		if isFullBudget(b) {
			return "full"
		}
		return strconv.FormatInt(budgetCateID(b), 10)
	}
	old := make(map[string]budgetItem, len(before))
	for _, b := range before {
		old[key(b)] = b
	}
	var out []string
	for _, b := range after {
		k := key(b)
		if o, ok := old[k]; !ok {
			out = append(out, fmt.Sprintf("新增 %s：%s", budgetLabel(b, cats), money(budgetMoney(b))))
		} else if budgetMoney(o) != budgetMoney(b) {
			out = append(out, fmt.Sprintf("修改 %s：%s → %s", budgetLabel(b, cats), money(budgetMoney(o)), money(budgetMoney(b))))
		}
		delete(old, k)
	}
	for _, b := range before {
		if _, gone := old[key(b)]; gone {
			out = append(out, fmt.Sprintf("删除 %s（原 %s）", budgetLabel(b, cats), money(budgetMoney(b))))
		}
	}
	return out
}

// budgetChangelist is Budget.toAPIJson over the whole list.
func budgetChangelist(list []budgetItem) string {
	out := cloneBudgets(list)
	for _, b := range out {
		delete(b, "category")
		delete(b, "createtime")
	}
	s, _ := json.Marshal(out)
	return string(s)
}

// --- API ---

// bookRange returns the book's config.range ("" for a normal month). It fails
// rather than guessing: the server scopes budgets by this value.
func bookRange(cfg *Config, book string) (string, error) {
	env, err := newClient(cfg).call("book", "list", map[string]string{"t": "-1"})
	if err != nil {
		return "", err
	}
	var w struct {
		List []struct {
			Bookid json.Number `json:"bookid"`
			Config *struct {
				Range string `json:"range"`
			} `json:"config"`
		} `json:"list"`
	}
	if err := json.Unmarshal(env.Data, &w); err != nil {
		return "", fmt.Errorf("解析账本列表失败：%w", err)
	}
	for _, b := range w.List {
		if b.Bookid.String() == book {
			if b.Config == nil {
				return "", nil
			}
			return b.Config.Range, nil
		}
	}
	return "", fmt.Errorf("找不到账本 %s（用 qianji books 查）", book)
}

func budgetParams(book, rng string, p budgetPeriod) map[string]string {
	params := map[string]string{"bookid": book, "flts": p.flts()}
	if rng != "" {
		params["range"] = rng
	}
	return params
}

// budgetSetArgs is one requested edit, from either the CLI or the MCP tool.
type budgetSetArgs struct {
	book, month, year, cate string
	total, money            *float64
}

// budgetPlan is a validated write that hasn't been sent.
type budgetPlan struct {
	book, rng string
	period    budgetPeriod
	cats      map[int64]cateInfo
	list      []budgetItem
	changes   []string
}

// prepareBudgetSet fetches the period and computes the full list to submit. It
// sends nothing and stops at the first failed read: budget/submitv2 replaces the
// whole period, so a partial picture of it must never reach the server.
func prepareBudgetSet(cfg *Config, a budgetSetArgs, now time.Time) (*budgetPlan, error) {
	bookID, err := strconv.ParseInt(a.book, 10, 64)
	if err != nil || bookID <= 0 {
		return nil, fmt.Errorf("需要账本 id（整数，用 qianji books 查），收到 %q", a.book)
	}
	if a.total != nil && (a.cate != "" || a.money != nil) {
		return nil, fmt.Errorf("总预算和分类预算一次只能改一个")
	}
	if a.total == nil && (a.cate == "" || a.money == nil) {
		return nil, fmt.Errorf("需要总预算金额，或者分类加金额（金额为 0 表示删除该分类预算）")
	}
	if (a.total != nil && *a.total < 0) || (a.money != nil && *a.money < 0) {
		return nil, fmt.Errorf("金额不能为负数")
	}

	rng, err := bookRange(cfg, a.book)
	if err != nil {
		return nil, err
	}
	period, err := resolveBudgetPeriod(a.month, a.year, rng, now)
	if err != nil {
		return nil, err
	}
	env, err := newClient(cfg).call("budget", "list", budgetParams(a.book, rng, period))
	if err != nil {
		return nil, err
	}
	before, err := decodeBudgets(env.Data)
	if err != nil {
		return nil, err
	}
	for _, b := range before {
		if budgetInt(b, "type", 0) != int64(period.budgetType()) || fmt.Sprint(b["typeinfo"]) != period.typeInfo() {
			return nil, fmt.Errorf("接口返回了其它周期的预算（type=%v typeinfo=%v），已中止", b["type"], b["typeinfo"])
		}
	}
	cats, err := loadExpenseCategories(cfg, a.book)
	if err != nil {
		return nil, err
	}

	ch := budgetChange{total: a.total}
	if a.total == nil {
		if ch.cate, err = resolveBudgetCate(a.cate, cats, before); err != nil {
			return nil, err
		}
		ch.money = *a.money
	}
	ctx := budgetContext{period: period, book: bookID, uid: cfg.UID}
	after, err := applyBudgetChange(cloneBudgets(before), ch, cats, ctx)
	if err != nil {
		return nil, err
	}
	// The app omits changelist for an empty list, and what the server does then
	// is unverified, so don't send it.
	if len(after) == 0 {
		return nil, fmt.Errorf("这是当期唯一的预算；删除后列表为空，这种提交的效果未验证，已中止")
	}
	changes := budgetDiff(before, after, cats)
	if len(changes) == 0 {
		return nil, fmt.Errorf("%s 的预算没有变化", period)
	}
	return &budgetPlan{book: a.book, rng: rng, period: period, cats: cats, list: after, changes: changes}, nil
}

func submitBudgetPlan(cfg *Config, p *budgetPlan) (*Envelope, error) {
	params := budgetParams(p.book, p.rng, p.period)
	params["changelist"] = budgetChangelist(p.list)
	return newClient(cfg).call("budget", "submitv2", params)
}

// --- CLI ---

func cmdBudget(cfg *Config, args []string) {
	requireLogin(cfg)
	if len(args) > 0 && args[0] == "set" {
		cmdBudgetSet(cfg, args[1:])
		return
	}
	var book, month, year string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--book":
			book = budgetFlagValue(args, &i)
		case "--month":
			month = budgetFlagValue(args, &i)
		case "--year":
			year = budgetFlagValue(args, &i)
		default:
			fatal(fmt.Errorf("未知参数 %q\n用法：qianji budget --book <id> [--month 2026-11 | --year 2026]", args[i]))
		}
	}
	if book == "" {
		fatal(fmt.Errorf("需要 --book <账本id>（用 qianji books 查）"))
	}
	rng, err := bookRange(cfg, book)
	if err != nil {
		fatal(err)
	}
	period, err := resolveBudgetPeriod(month, year, rng, time.Now())
	if err != nil {
		fatal(err)
	}
	env, err := newClient(cfg).call("budget", "list", budgetParams(book, rng, period))
	if err != nil {
		fatal(err)
	}
	if outputMode != "human" {
		emit(env, nil)
		return
	}
	cats, err := loadExpenseCategories(cfg, book)
	if err != nil {
		fatal(err)
	}
	emit(env, renderBudgets(period, cats))
}

// budgetFlagValue is strict where other commands are lenient: a typo or a missing
// value must not make a write silently fall back to the current month.
func budgetFlagValue(args []string, i *int) string {
	if *i+1 >= len(args) {
		fatal(fmt.Errorf("%s 缺少取值", args[*i]))
	}
	*i++
	return args[*i]
}

func budgetFlagMoney(args []string, i *int) *float64 {
	flag := args[*i]
	v, err := strconv.ParseFloat(budgetFlagValue(args, i), 64)
	if err != nil {
		fatal(fmt.Errorf("%s 要是数字", flag))
	}
	return &v
}

// cmdBudgetSet changes one limit. Dry-run by default; --confirm plus an
// interactive yes to submit.
func cmdBudgetSet(cfg *Config, args []string) {
	var a budgetSetArgs
	confirm := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--book":
			a.book = budgetFlagValue(args, &i)
		case "--month":
			a.month = budgetFlagValue(args, &i)
		case "--year":
			a.year = budgetFlagValue(args, &i)
		case "--cate":
			a.cate = budgetFlagValue(args, &i)
		case "--total":
			a.total = budgetFlagMoney(args, &i)
		case "--money":
			a.money = budgetFlagMoney(args, &i)
		case "--confirm", "--yes":
			confirm = true
		default:
			fatal(fmt.Errorf("未知参数 %q\n用法：qianji budget set --book <id> [--month 2026-11 | --year 2026] (--total <m> | --cate <名/id> --money <m>) [--confirm]", args[i]))
		}
	}
	plan, err := prepareBudgetSet(cfg, a, time.Now())
	if err != nil {
		fatal(err)
	}
	summary := plan.period.String() + "：\n  " + strings.Join(plan.changes, "\n  ")
	if !confirm {
		fmt.Fprintln(os.Stderr, "DRY-RUN（不会写入）。将提交的 changelist（整期覆盖）：")
		printJSON([]byte(budgetChangelist(plan.list)))
		fmt.Fprintf(os.Stderr, "\n改动 %s\n确认无误后加 --confirm 真正写入。\n", summary)
		return
	}
	fmt.Fprintf(os.Stderr, "即将修改真实预算 %s\n输入 yes 确认：", summary)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "yes" {
		fmt.Fprintln(os.Stderr, "已取消，未写入。")
		return
	}
	env, err := submitBudgetPlan(cfg, plan)
	if err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, "已写入。服务器返回：")
	emit(env, renderBudgets(plan.period, plan.cats))
}

// --- MCP ---

// aRef reads an argument that may arrive as a string or a number, such as an id.
func aRef(a map[string]any, k string) string {
	if s := aStr(a, k, ""); s != "" {
		return s
	}
	if v, ok := a[k].(float64); ok {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func budgetToolRun(cfg *Config, a map[string]any) (string, error) {
	book := aRef(a, "bookid")
	if book == "" {
		return "", fmt.Errorf("bookid 为必填")
	}
	rng, err := bookRange(cfg, book)
	if err != nil {
		return "", err
	}
	period, err := resolveBudgetPeriod(aStr(a, "month", ""), aRef(a, "year"), rng, time.Now())
	if err != nil {
		return "", err
	}
	return callData(cfg, "budget", "list", budgetParams(book, rng, period))
}

// budgetSetToolRun previews unless confirm=true. Unlike qianji_add, a mistake here
// rewrites a whole period, so the default is the safe side.
func budgetSetToolRun(cfg *Config, a map[string]any) (string, error) {
	args := budgetSetArgs{book: aRef(a, "bookid"), month: aStr(a, "month", ""), year: aRef(a, "year"), cate: aRef(a, "cate")}
	if v, ok := aNum(a, "total"); ok {
		args.total = &v
	}
	if v, ok := aNum(a, "money"); ok {
		args.money = &v
	}
	plan, err := prepareBudgetSet(cfg, args, time.Now())
	if err != nil {
		return "", err
	}
	if confirm, _ := a["confirm"].(bool); !confirm {
		out, _ := json.Marshal(map[string]any{
			"dry_run":    true,
			"period":     plan.period.String(),
			"changes":    plan.changes,
			"changelist": json.RawMessage(budgetChangelist(plan.list)),
		})
		return string(out), nil
	}
	env, err := submitBudgetPlan(cfg, plan)
	if err != nil {
		return "", err
	}
	return string(env.Data), nil
}
