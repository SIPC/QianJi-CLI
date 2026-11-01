package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// outputMode is the global output format: "human" (default), "json", or "raw".
var outputMode = "human"

// parseOutputFlags removes --raw/--json/--human from args and sets outputMode.
func parseOutputFlags(args []string) []string {
	kept := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--raw":
			outputMode = "raw"
		case "--json":
			outputMode = "json"
		case "--human":
			outputMode = "human"
		default:
			kept = append(kept, a)
		}
	}
	return kept
}

// emit renders an envelope per outputMode: raw prints the untouched response
// body, json pretty-prints the data, human calls render (falling back to pretty
// JSON when render is nil).
func emit(env *Envelope, render func(json.RawMessage)) {
	switch outputMode {
	case "raw":
		os.Stdout.Write(env.Raw)
		if n := len(env.Raw); n == 0 || env.Raw[n-1] != '\n' {
			fmt.Println()
		}
	case "json":
		printJSON(env.Data)
	default:
		if render != nil {
			render(env.Data)
		} else {
			printJSON(env.Data)
		}
	}
}

// --- value formatting ---

func fmtTime(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04")
}

func fmtDate(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}

func money(v float64) string { return fmt.Sprintf("%.2f", v) }

var billTypeText = map[int]string{
	0: "支出", 1: "收入", 2: "转账", 3: "信用卡还款", 4: "收款", 5: "报销",
	6: "债务", 7: "借出", 9: "还债", 10: "利息支出", 11: "利息收入",
	20: "退款", 21: "已报销", 22: "打包报销",
}

func billType(t int) string {
	if s, ok := billTypeText[t]; ok {
		return s
	}
	return fmt.Sprintf("类型%d", t)
}

var billStatusText = map[int]string{0: "已删除", 1: "正常", 2: "未同步"}

func billStatus(s int) string {
	if v, ok := billStatusText[s]; ok {
		return v
	}
	return fmt.Sprintf("状态%d", s)
}

func genderText(g int) string {
	switch g {
	case 0:
		return "女"
	case 1:
		return "男"
	default:
		return "未设置"
	}
}

// vipTypeText maps viptype to the member name from vip/config.types (name2).
var vipTypeText = map[int]string{
	0: "非会员", 2: "月度会员", 3: "半年会员", 4: "年度会员", 100: "终身会员",
}

func vipText(t int) string {
	if s, ok := vipTypeText[t]; ok {
		return s
	}
	if t <= 0 {
		return "非会员"
	}
	return fmt.Sprintf("会员 (type %d)", t)
}

// --- renderers (human mode) ---

func renderMe(data json.RawMessage) {
	var u struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Avatar   string `json:"avatar"`
		Weixinid string `json:"weixinid"`
		Gender   int    `json:"gender"`
		Viptype  int    `json:"viptype"`
		Time     int64  `json:"time"`
		Vipstart int64  `json:"vipstart"`
		Vipend   int64  `json:"vipend"`
	}
	if json.Unmarshal(data, &u) != nil {
		printJSON(data)
		return
	}
	fmt.Printf("昵称:  %s\n", u.Name)
	fmt.Printf("uid:   %s\n", u.ID)
	fmt.Printf("邮箱:  %s\n", u.Email)
	fmt.Printf("性别:  %s\n", genderText(u.Gender))
	fmt.Printf("注册:  %s\n", fmtTime(u.Time))
	fmt.Printf("会员:  %s（%s 至 %s）\n", vipText(u.Viptype), fmtDate(u.Vipstart), fmtDate(u.Vipend))
	if u.Weixinid != "" {
		fmt.Println("微信:  已绑定")
	}
	fmt.Printf("头像:  %s\n", u.Avatar)
}

func renderBooks(data json.RawMessage) {
	var w struct {
		List []struct {
			Bookid      int64  `json:"bookid"`
			Name        string `json:"name"`
			Typename    string `json:"typename"`
			Membercount int    `json:"membercount"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	if len(w.List) == 0 {
		fmt.Println("(无账本)")
		return
	}
	for _, b := range w.List {
		fmt.Printf("%-16s  id=%-14d %s  成员 %d\n", b.Name, b.Bookid, b.Typename, b.Membercount)
	}
}

func renderAssets(data json.RawMessage) {
	var w struct {
		List []struct {
			Name     string  `json:"name"`
			Money    float64 `json:"money"`
			Currency string  `json:"currency"`
			Id       int64   `json:"id"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	if len(w.List) == 0 {
		fmt.Println("(无资产账户)")
		return
	}
	for _, a := range w.List {
		fmt.Printf("%-16s  %12s %s  id=%d\n", a.Name, money(a.Money), a.Currency, a.Id)
	}
}

func renderCategories(data json.RawMessage) {
	var w struct {
		List []struct {
			Name    string `json:"name"`
			Id      int64  `json:"id"`
			Sublist []struct {
				Name string `json:"name"`
				Id   int64  `json:"id"`
			} `json:"sublist"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	if len(w.List) == 0 {
		fmt.Println("(无分类)")
		return
	}
	for _, c := range w.List {
		fmt.Printf("%s  (id=%d)\n", c.Name, c.Id)
		for _, s := range c.Sublist {
			fmt.Printf("  └ %s  (id=%d)\n", s.Name, s.Id)
		}
	}
}

func renderLogins(data json.RawMessage) {
	var w struct {
		List []struct {
			Name      string `json:"name"`
			Deviceid  string `json:"deviceid"`
			Time      int64  `json:"time"`
			Iscurrent int    `json:"iscurrent"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	for _, d := range w.List {
		cur := ""
		if d.Iscurrent == 1 {
			cur = "  ← 当前"
		}
		did := d.Deviceid
		if len(did) > 8 {
			did = did[:8]
		}
		fmt.Printf("%s  %-24s %s%s\n", fmtTime(d.Time), d.Name, did, cur)
	}
}

type billRow struct {
	Time      int64    `json:"time"`
	Type      int      `json:"type"`
	Money     float64  `json:"money"`
	Cateid    int64    `json:"cateid"`
	Fromact   string   `json:"fromact"`
	Targetact string   `json:"targetact"`
	Remark    string   `json:"remark"`
	Descinfo  string   `json:"descinfo"`
	Images    []string `json:"images"`
	Status    int      `json:"status"`
}

// billFilter is applied locally to the pulled/cached bills before rendering.
type billFilter struct {
	since, until int64   // unix; until is an exclusive upper bound
	typ          int     // -1 = any
	cate         int64   // 0 = any (resolved from cateArg)
	cateArg      string  // --cate value: a category name or id
	min, max     float64 // max 0 = no upper bound
	hasMin       bool
	keyword      string
	limit        int // 0 = all
}

func (f *billFilter) match(b billRow) bool {
	if f.since > 0 && b.Time < f.since {
		return false
	}
	if f.until > 0 && b.Time >= f.until {
		return false
	}
	if f.typ >= 0 && b.Type != f.typ {
		return false
	}
	if f.cate > 0 && b.Cateid != f.cate {
		return false
	}
	if f.hasMin && b.Money < f.min {
		return false
	}
	if f.max > 0 && b.Money > f.max {
		return false
	}
	if f.keyword != "" {
		if !strings.Contains(b.Remark, f.keyword) && !strings.Contains(b.Descinfo, f.keyword) &&
			!strings.Contains(b.Fromact, f.keyword) && !strings.Contains(b.Targetact, f.keyword) {
			return false
		}
	}
	return true
}

func applyBillFilter(rows []billRow, f *billFilter) []billRow {
	if f == nil {
		return rows
	}
	out := make([]billRow, 0, len(rows))
	for _, b := range rows {
		if f.match(b) {
			out = append(out, b)
		}
	}
	return out
}

// renderBillRows prints bills accumulated across pages, newest first, plus a
// summary. truncated means a --pages cap stopped the pull before hasmore=0.
func renderBillRows(rows []billRow, deletes int, truncated bool, limit int, cats map[int64]string) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Time > rows[j].Time })
	total := len(rows)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	for _, b := range rows {
		note := b.Remark
		if note == "" && b.Descinfo != b.Fromact {
			note = b.Descinfo
		}
		acct := b.Fromact
		if b.Targetact != "" {
			acct = b.Fromact + "→" + b.Targetact
		}
		line := fmt.Sprintf("%s  %-8s %10s  %s", fmtTime(b.Time), billType(b.Type), money(b.Money), acct)
		if c := cats[b.Cateid]; c != "" {
			line += "  [" + c + "]"
		}
		if note != "" {
			line += "  " + note
		}
		if len(b.Images) > 0 {
			line += fmt.Sprintf("  [图×%d]", len(b.Images))
		}
		if b.Status != 1 {
			line += "  (" + billStatus(b.Status) + ")"
		}
		fmt.Println(line)
	}
	tail := fmt.Sprintf("— 共 %d 条", total)
	if limit > 0 && total > limit {
		tail += fmt.Sprintf("，显示最近 %d 条", limit)
	}
	if deletes > 0 {
		tail += fmt.Sprintf("，另有 %d 条已删除", deletes)
	}
	if truncated {
		tail += "，已达 --pages 上限（增大可拉更多）"
	}
	fmt.Println(tail)
}

func freqText(f string) string {
	switch f {
	case "day":
		return "日"
	case "week":
		return "周"
	case "month":
		return "月"
	case "year":
		return "年"
	default:
		return f
	}
}

func renderSaving(data json.RawMessage) {
	var w struct {
		List []struct {
			Name     string `json:"name"`
			Currency string `json:"currency"`
			Remark   string `json:"remark"`
			Rule     struct {
				Amount    float64 `json:"amount"`
				Frequency string  `json:"frequency"`
			} `json:"rule"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	if len(w.List) == 0 {
		fmt.Println("(无存钱计划)")
		return
	}
	for _, s := range w.List {
		line := fmt.Sprintf("%-20s 目标 %s %s", s.Name, money(s.Rule.Amount), s.Currency)
		if s.Rule.Frequency != "" {
			line += "  每" + freqText(s.Rule.Frequency)
		}
		if s.Remark != "" {
			line += "  " + s.Remark
		}
		fmt.Println(line)
	}
}

func renderCurrency(data json.RawMessage) {
	var w struct {
		List []struct {
			Symbol    string `json:"symbol"`
			Name      string `json:"name"`
			Sign      string `json:"sign"`
			Baseprice string `json:"baseprice"`
			Iscommon  int    `json:"iscommon"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	for _, c := range w.List {
		star := "  "
		if c.Iscommon == 1 {
			star = "* "
		}
		fmt.Printf("%s%-5s %-8s %-3s 汇率 %s\n", star, c.Symbol, c.Name, c.Sign, c.Baseprice)
	}
	fmt.Printf("— 共 %d 种（* 常用，汇率相对基准货币）\n", len(w.List))
}

func renderRepeat(data json.RawMessage) {
	var w struct {
		List []struct {
			Count int `json:"count"`
			Data  struct {
				Money    float64 `json:"money"`
				Category struct {
					Name string `json:"name"`
				} `json:"category"`
				Repeat struct {
					Type  string   `json:"type"`
					Value []string `json:"value"`
				} `json:"repeat"`
			} `json:"data"`
		} `json:"list"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	if len(w.List) == 0 {
		fmt.Println("(无周期记账)")
		return
	}
	for _, r := range w.List {
		rep := "每" + freqText(r.Data.Repeat.Type)
		if len(r.Data.Repeat.Value) > 0 {
			rep += " " + strings.Join(r.Data.Repeat.Value, ",") + "号"
		}
		fmt.Printf("%-16s %10s  %s  已执行 %d 次\n", r.Data.Category.Name, money(r.Data.Money), rep, r.Count)
	}
}

// renderBudgets prints the total, then each category under its parent. Like the
// app's header, a total with no limit of its own shows the category figures.
func renderBudgets(p budgetPeriod, cats map[int64]cateInfo) func(json.RawMessage) {
	return func(data json.RawMessage) {
		items, err := decodeBudgets(data)
		if err != nil {
			printJSON(data)
			return
		}
		fmt.Printf("%s 预算\n", p)
		if len(items) == 0 {
			fmt.Println("(当期未设预算)")
			return
		}
		list := normalizeBudgets(cloneBudgets(items), cats, func() budgetItem {
			return budgetItem{"flag": budgetFlagFull, "money": 0.0}
		})
		for _, b := range list {
			limit := budgetMoney(b)
			used, hasUsed := budgetNum(b, "used")
			label := budgetLabel(b, cats)
			if isFullBudget(b) {
				cateLimit, _ := budgetNum(b, "totalCateLimit")
				if limit <= cateLimit {
					used, _ = budgetNum(b, "totalCateUsed")
					hasUsed = true
					label += "（分类合计）"
				}
			} else if c, ok := cats[budgetCateID(b)]; ok && c.Parent != -1 {
				label = "  └ " + label
			}
			line := fmt.Sprintf("%-16s %10s", label, money(limit))
			if hasUsed {
				line += "  已用 " + money(used)
				if left := limit - used; left >= 0 {
					line += "  剩余 " + money(left)
				} else {
					line += "  超支 " + money(-left)
				}
			}
			if n := budgetInt(b, "count", 0); n > 0 {
				line += fmt.Sprintf("  %d 笔", n)
			}
			fmt.Println(line)
			if v, _ := budgetNum(b, "notbudget"); isFullBudget(b) && v > 0 {
				fmt.Printf("  不计入预算 %s\n", money(v))
			}
		}
	}
}

func renderInstallment(data json.RawMessage) {
	var w struct {
		List []json.RawMessage `json:"list"`
	}
	if json.Unmarshal(data, &w) == nil && len(w.List) == 0 {
		fmt.Println("(无分期)")
		return
	}
	printJSON(data) // fields unconfirmed (no sample); show JSON until modelled
}
