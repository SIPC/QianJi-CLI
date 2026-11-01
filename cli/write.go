package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// billSyncKey is the outer key of bill/syncall's v: {billSyncKey: {changelist,
// dellist}}. The app's bh/a.getKey() returns the literal "bills".
const billSyncKey = "bills"

// cmdAdd writes one bill via bill/syncall. It is DRY-RUN by default (prints the
// v payload it would send) and only submits with --confirm, after an interactive
// yes. Bill fields mirror Bill.toSyncJson() recovered from the app; the outer
// key of v is the login user id (recovered from gh/a.a). A wrong key/field is
// rejected by the server (ec != 200) rather than creating a bad bill.
func cmdAdd(cfg *Config, args []string) {
	requireLogin(cfg)
	typ, moneyStr, cate, asset, remark, timeStr := "", "", "", "", "", ""
	book := "-1"
	confirm := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--type":
			if i+1 < len(args) {
				typ = args[i+1]
				i++
			}
		case "--money":
			if i+1 < len(args) {
				moneyStr = args[i+1]
				i++
			}
		case "--cate", "--cateid":
			if i+1 < len(args) {
				cate = args[i+1]
				i++
			}
		case "--asset", "--assetid":
			if i+1 < len(args) {
				asset = args[i+1]
				i++
			}
		case "--book":
			if i+1 < len(args) {
				book = args[i+1]
				i++
			}
		case "--remark":
			if i+1 < len(args) {
				remark = args[i+1]
				i++
			}
		case "--time":
			if i+1 < len(args) {
				timeStr = args[i+1]
				i++
			}
		case "--confirm", "--yes":
			confirm = true
		}
	}
	if typ == "" || moneyStr == "" || cate == "" || asset == "" {
		fatal(fmt.Errorf("需要 --type、--money、--cate <分类id>、--asset <资产id>\n（分类 id 用 `qianji categories --book <id>` 查，资产 id 用 `qianji assets` 查）"))
	}
	typeInt, err := strconv.Atoi(typ)
	if err != nil {
		fatal(fmt.Errorf("--type 要是整数：0 支出 / 1 收入 / 2 转账 / 3 还款 ..."))
	}
	moneyF, err := strconv.ParseFloat(moneyStr, 64)
	if err != nil || moneyF <= 0 {
		fatal(fmt.Errorf("--money 要是正数"))
	}
	cateID, err := strconv.ParseInt(cate, 10, 64)
	if err != nil {
		fatal(fmt.Errorf("--cate 要是分类 id（整数）"))
	}
	assetID, err := strconv.ParseInt(asset, 10, 64)
	if err != nil {
		fatal(fmt.Errorf("--asset 要是资产 id（整数）"))
	}
	bookID, err := strconv.ParseInt(book, 10, 64)
	if err != nil {
		fatal(fmt.Errorf("--book 要是账本 id（整数）"))
	}

	t := time.Now()
	if timeStr != "" {
		parsed, perr := parseBillTime(timeStr)
		if perr != nil {
			fatal(perr)
		}
		t = parsed
	}
	now := time.Now()
	// Bill id in the real data is unix-millis * 10000 + a 4-digit serial.
	id := now.UnixMilli()*10000 + int64(now.Nanosecond()/1e5%10000)

	bill := map[string]any{
		"id":         id,
		"userid":     cfg.UID,
		"bookid":     bookID,
		"time":       t.Unix(),
		"type":       typeInt,
		"money":      moneyF,
		"cateid":     cateID,
		"assetid":    assetID,
		"remark":     remark,
		"status":     2, // 2 = not-yet-synced; server treats it as a new bill
		"createtime": now.Unix(),
		"updatetime": now.Unix(),
		"platform":   0, // manual
	}
	inner := map[string]any{
		"changelist": []any{bill},
		"dellist":    []any{},
	}
	v := map[string]any{billSyncKey: inner}
	vb, _ := json.Marshal(v)

	summary := fmt.Sprintf("%s %.2f  分类 %d  资产 %d  账本 %d  %s  备注 %q",
		billType(typeInt), moneyF, cateID, assetID, bookID, t.Format("2006-01-02 15:04"), remark)

	if !confirm {
		fmt.Fprintln(os.Stderr, "DRY-RUN（不会写入）。将提交的 v：")
		printJSON(vb)
		fmt.Fprintf(os.Stderr, "\n摘要：%s\n确认无误后加 --confirm 真正写入。\n", summary)
		return
	}

	fmt.Fprintf(os.Stderr, "即将写入一笔真实账单：\n  %s\n输入 yes 确认：", summary)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "yes" {
		fmt.Fprintln(os.Stderr, "已取消，未写入。")
		return
	}
	env, err := newClient(cfg).call("bill", "syncall", map[string]string{"v": string(vb)})
	if err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "已写入，账单 id：%d（如需撤销：qianji del %d --confirm）\n服务器返回：\n", id, id)
	printJSON(env.Data)
}

// parseBillTime accepts yyyy-MM-dd[ HH:mm[:ss]] in local time.
func parseBillTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--time 格式应为 2006-01-02 或 2006-01-02 15:04")
}

// cmdDel deletes bills by id via bill/syncall's dellist. Dry-run by default;
// --confirm plus an interactive yes to actually delete.
func cmdDel(cfg *Config, args []string) {
	requireLogin(cfg)
	var ids []string
	confirm := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--confirm", "--yes":
			confirm = true
		default:
			ids = append(ids, args[i])
		}
	}
	if len(ids) == 0 {
		fatal(fmt.Errorf("用法：qianji del <billid> [<billid> ...] [--confirm]"))
	}
	dellist := make([]any, 0, len(ids))
	for _, s := range ids {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			fatal(fmt.Errorf("billid 要是整数：%q", s))
		}
		dellist = append(dellist, n)
	}
	v := map[string]any{billSyncKey: map[string]any{"changelist": []any{}, "dellist": dellist}}
	vb, _ := json.Marshal(v)

	if !confirm {
		fmt.Fprintln(os.Stderr, "DRY-RUN（不会删除）。将提交的 v：")
		printJSON(vb)
		fmt.Fprintf(os.Stderr, "\n将删除 %d 笔账单：%s\n确认后加 --confirm。\n", len(ids), strings.Join(ids, ", "))
		return
	}
	fmt.Fprintf(os.Stderr, "即将删除 %d 笔真实账单：%s\n输入 yes 确认：", len(ids), strings.Join(ids, ", "))
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "yes" {
		fmt.Fprintln(os.Stderr, "已取消，未删除。")
		return
	}
	env, err := newClient(cfg).call("bill", "syncall", map[string]string{"v": string(vb)})
	if err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, "已提交。服务器返回：")
	printJSON(env.Data)
}
