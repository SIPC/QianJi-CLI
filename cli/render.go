package main

import (
	"encoding/json"
	"fmt"
	"os"
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

func vipText(t int) string {
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

func renderBills(data json.RawMessage) {
	var w struct {
		Changes []struct {
			Time      int64    `json:"time"`
			Type      int      `json:"type"`
			Money     float64  `json:"money"`
			Fromact   string   `json:"fromact"`
			Targetact string   `json:"targetact"`
			Remark    string   `json:"remark"`
			Descinfo  string   `json:"descinfo"`
			Images    []string `json:"images"`
			Status    int      `json:"status"`
		} `json:"changes"`
		Deletes []int64 `json:"deletes"`
		Hasmore int     `json:"hasmore"`
	}
	if json.Unmarshal(data, &w) != nil {
		printJSON(data)
		return
	}
	for _, b := range w.Changes {
		note := b.Remark
		if note == "" && b.Descinfo != b.Fromact {
			note = b.Descinfo
		}
		acct := b.Fromact
		if b.Targetact != "" {
			acct = b.Fromact + "→" + b.Targetact
		}
		line := fmt.Sprintf("%s  %-8s %10s  %s", fmtTime(b.Time), billType(b.Type), money(b.Money), acct)
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
	tail := fmt.Sprintf("— %d 条", len(w.Changes))
	if len(w.Deletes) > 0 {
		tail += fmt.Sprintf("，另有 %d 条已删除", len(w.Deletes))
	}
	if w.Hasmore == 1 {
		tail += "，还有更多（加 --pages N 拉更多页）"
	}
	fmt.Println(tail)
}
