package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testCats = map[int64]cateInfo{
	10: {ID: 10, Parent: -1, Name: "餐饮"},
	11: {ID: 11, Parent: 10, Name: "外卖"},
	12: {ID: 12, Parent: 10, Name: "咖啡"},
	20: {ID: 20, Parent: -1, Name: "交通"},
	21: {ID: 21, Parent: 20, Name: "打车"},
	31: {ID: 31, Parent: 20, Name: "咖啡"}, // same name as 12
}

var testCtx = budgetContext{period: budgetPeriod{2026, 3}, book: 7, uid: "u1"}

func cateBudget(id int64, m float64) budgetItem {
	return budgetItem{"flag": budgetFlagCategory, "cateid": id, "money": m}
}

func fullBudget(m float64) budgetItem {
	return budgetItem{"flag": budgetFlagFull, "cateid": int64(-1), "money": m}
}

func TestBudgetPeriodFormats(t *testing.T) {
	p := budgetPeriod{2026, 3}
	if p.typeInfo() != "2026,3" { // zero-padding would address another period
		t.Errorf("typeInfo = %q", p.typeInfo())
	}
	if p.flts() != `{"month":"2026,3"}` {
		t.Errorf("flts = %s", p.flts())
	}
	y := budgetPeriod{year: 2026}
	if y.flts() != `{"year":"2026"}` || y.budgetType() != budgetTypeYear {
		t.Errorf("year flts = %s type = %d", y.flts(), y.budgetType())
	}
}

func TestResolveBudgetPeriod(t *testing.T) {
	jan5 := time.Date(2026, 1, 5, 12, 0, 0, 0, time.Local)
	cases := []struct {
		name, month, year, rng string
		now                    time.Time
		want                   budgetPeriod
		wantErr                bool
	}{
		{name: "default", now: jan5, want: budgetPeriod{2026, 1}},
		{name: "before custom start rolls back across a year", rng: "m10", now: jan5, want: budgetPeriod{2025, 12}},
		{name: "after custom start", rng: "m3", now: jan5, want: budgetPeriod{2026, 1}},
		{name: "explicit month", month: "2026-3", now: jan5, want: budgetPeriod{2026, 3}},
		{name: "padded month", month: "2026-03", now: jan5, want: budgetPeriod{2026, 3}},
		{name: "year", year: "2030", now: jan5, want: budgetPeriod{year: 2030}},
		{name: "both", month: "2026-3", year: "2026", now: jan5, wantErr: true},
		{name: "bad month", month: "2026-13", now: jan5, wantErr: true},
		{name: "not a month", month: "2026", now: jan5, wantErr: true},
		{name: "bad year", year: "26x", now: jan5, wantErr: true},
	}
	for _, c := range cases {
		got, err := resolveBudgetPeriod(c.month, c.year, c.rng, c.now)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%s: got %v, %v", c.name, got, err)
		}
	}
}

func TestNormalizeRaisesLimits(t *testing.T) {
	list := normalizeBudgets([]budgetItem{
		cateBudget(11, 300), cateBudget(12, 400), cateBudget(10, 500), fullBudget(600),
	}, testCats, testCtx.newFull)

	// Order: total, parent, subs.
	want := []int64{-1, 10, 11, 12}
	for i, id := range want {
		if budgetCateID(list[i]) != id {
			t.Fatalf("order[%d] = %d, want %d", i, budgetCateID(list[i]), id)
		}
	}
	if budgetMoney(list[1]) != 700 {
		t.Errorf("parent = %v, want raised to the subs' 700", budgetMoney(list[1]))
	}
	if budgetMoney(list[0]) != 700 {
		t.Errorf("total = %v, want raised to the categories' 700", budgetMoney(list[0]))
	}
}

func TestNormalizeCountsEachCategoryOnce(t *testing.T) {
	list := normalizeBudgets([]budgetItem{
		fullBudget(5000), cateBudget(10, 500), cateBudget(11, 100), cateBudget(21, 80),
	}, testCats, testCtx.newFull)
	// 10 covers its sub 11; 21's parent has no budget, so it counts itself.
	if v, _ := budgetNum(list[0], "totalCateLimit"); v != 580 {
		t.Errorf("totalCateLimit = %v, want 580", v)
	}
	if budgetMoney(list[0]) != 5000 {
		t.Errorf("an independent total must stay put, got %v", budgetMoney(list[0]))
	}
}

func TestNormalizeAddsMissingTotal(t *testing.T) {
	list := normalizeBudgets([]budgetItem{cateBudget(21, 80)}, testCats, testCtx.newFull)
	if len(list) != 2 || !isFullBudget(list[0]) || budgetMoney(list[0]) != 80 {
		t.Fatalf("list = %v", list)
	}
	if list[0]["typeinfo"] != "2026,3" || list[0]["bookid"] != int64(7) || list[0]["userid"] != "u1" {
		t.Errorf("new total = %v", list[0])
	}
}

func TestNormalizeKeepsUnknownCategory(t *testing.T) {
	list := normalizeBudgets([]budgetItem{fullBudget(100), cateBudget(999, 50)}, testCats, testCtx.newFull)
	if len(list) != 2 || budgetCateID(list[1]) != 999 {
		t.Fatalf("list = %v", list)
	}
	if budgetMoney(list[0]) != 100 {
		t.Errorf("unknown category must not raise the total, got %v", budgetMoney(list[0]))
	}
}

func TestApplyBudgetChange(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	start := func() []budgetItem { return []budgetItem{fullBudget(1000), cateBudget(10, 500), cateBudget(20, 200)} }

	if _, err := applyBudgetChange(start(), budgetChange{total: f(600)}, testCats, testCtx); err == nil {
		t.Error("a total below the category limits must be rejected")
	}
	list, err := applyBudgetChange(start(), budgetChange{total: f(800)}, testCats, testCtx)
	if err != nil || budgetMoney(list[0]) != 800 {
		t.Errorf("set total: %v %v", list, err)
	}
	list, err = applyBudgetChange(start(), budgetChange{cate: 20, money: 300}, testCats, testCtx)
	if err != nil || budgetMoney(list[2]) != 300 || len(list) != 3 {
		t.Errorf("update category: %v %v", list, err)
	}
	list, err = applyBudgetChange(start(), budgetChange{cate: 21, money: 50}, testCats, testCtx)
	if err != nil || len(list) != 4 || budgetCateID(list[3]) != 21 || list[3]["typeinfo"] != "2026,3" {
		t.Errorf("add category: %v %v", list, err)
	}
	list, err = applyBudgetChange(start(), budgetChange{cate: 20, money: 0}, testCats, testCtx)
	if err != nil || len(list) != 2 {
		t.Errorf("remove category: %v %v", list, err)
	}
	if _, err = applyBudgetChange(start(), budgetChange{cate: 21, money: 0}, testCats, testCtx); err == nil {
		t.Error("removing a budget that isn't set must be an error")
	}
	if _, err = applyBudgetChange(start(), budgetChange{cate: 999, money: 5}, testCats, testCtx); err == nil {
		t.Error("adding a budget on an unknown category must be an error")
	}
}

func TestResolveBudgetCate(t *testing.T) {
	if id, err := resolveBudgetCate("外卖", testCats, nil); err != nil || id != 11 {
		t.Errorf("by name: %d %v", id, err)
	}
	if id, err := resolveBudgetCate("21", testCats, nil); err != nil || id != 21 {
		t.Errorf("by id: %d %v", id, err)
	}
	if _, err := resolveBudgetCate("咖啡", testCats, nil); err == nil {
		t.Error("a repeated name must be an error")
	}
	if _, err := resolveBudgetCate("不存在", testCats, nil); err == nil {
		t.Error("an unknown name must be an error")
	}
	if _, err := resolveBudgetCate("999", testCats, nil); err == nil {
		t.Error("an unknown id must be an error")
	}
	if id, err := resolveBudgetCate("999", testCats, []budgetItem{cateBudget(999, 5)}); err != nil || id != 999 {
		t.Errorf("an id only in the budget list stays removable: %d %v", id, err)
	}
}

func TestBudgetDiffShowsRaisedLimits(t *testing.T) {
	before := []budgetItem{fullBudget(100), cateBudget(10, 100), cateBudget(20, 50)}
	after, err := applyBudgetChange(cloneBudgets(before), budgetChange{cate: 10, money: 300}, testCats, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(budgetDiff(before, after, testCats), "\n")
	for _, want := range []string{"修改 总预算：100.00 → 350.00", "修改 餐饮：100.00 → 300.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "交通") {
		t.Errorf("unchanged budget listed: %q", got)
	}
}

// fakeQianji serves just enough of the API, recording submissions.
type fakeQianji struct {
	budgets   string // body of data for budget/list
	listCode  int    // non-zero: ec to return from budget/list
	submitted []map[string]string
	listForm  map[string]string
}

func (f *fakeQianji) server(t *testing.T) (*httptest.Server, *Config) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		ok := func(data string) { _, _ = w.Write([]byte(`{"ec":200,"em":"","data":` + data + `}`)) }
		switch r.URL.Path {
		case "/book/list":
			ok(`{"list":[{"bookid":7,"name":"b","config":{"range":"m10"}},{"bookid":8,"name":"c","config":null}]}`)
		case "/category/listv2":
			ok(`{"list":[{"id":10,"name":"餐饮","sublist":[{"id":11,"name":"外卖"}]},{"id":20,"name":"交通"}]}`)
		case "/budget/list":
			f.listForm = form
			if f.listCode != 0 {
				_, _ = w.Write([]byte(`{"ec":500,"em":"boom"}`))
				return
			}
			ok(f.budgets)
		case "/budget/submitv2":
			f.submitted = append(f.submitted, form)
			ok(`{"list":[]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &Config{Host: srv.URL + "/", UID: "u1", Token: "tok", DevID: "dev"}
}

const fakeMarch = `{"list":[
	{"budgetid":1234567890123456789,"bookid":7,"userid":"u1","type":1,"typeinfo":"2026,3","flag":1,"cateid":-1,"money":1000,"used":120.5,"createtime":99,"futurefield":"keep"},
	{"budgetid":1234567890123456790,"bookid":7,"userid":"u1","type":1,"typeinfo":"2026,3","flag":2,"cateid":10,"money":500,"used":100,"category":{"id":10,"name":"餐饮"},"createtime":99}
]}`

func TestBudgetSetSubmitsWholePeriod(t *testing.T) {
	f := &fakeQianji{budgets: fakeMarch}
	_, cfg := f.server(t)
	m := 300.0
	plan, err := prepareBudgetSet(cfg, budgetSetArgs{book: "7", month: "2026-3", cate: "交通", money: &m}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if f.listForm["flts"] != `{"month":"2026,3"}` || f.listForm["range"] != "m10" || f.listForm["bookid"] != "7" {
		t.Errorf("list form = %v", f.listForm)
	}
	if len(f.submitted) != 0 {
		t.Fatal("prepare must not submit")
	}
	if _, err := submitBudgetPlan(cfg, plan); err != nil {
		t.Fatal(err)
	}
	if len(f.submitted) != 1 {
		t.Fatalf("submitted %d times", len(f.submitted))
	}
	got := f.submitted[0]
	if got["flts"] != `{"month":"2026,3"}` || got["range"] != "m10" {
		t.Errorf("submit form = %v", got)
	}
	cl := got["changelist"]
	// Existing budgets go back with ids intact and unknown fields kept.
	for _, want := range []string{"1234567890123456789", "1234567890123456790", `"futurefield":"keep"`, `"cateid":20`, `"money":300`} {
		if !strings.Contains(cl, want) {
			t.Errorf("changelist missing %s: %s", want, cl)
		}
	}
	for _, gone := range []string{`"category"`, `"createtime"`} {
		if strings.Contains(cl, gone) {
			t.Errorf("changelist must not contain %s: %s", gone, cl)
		}
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(cl), &items); err != nil || len(items) != 3 {
		t.Errorf("changelist = %s (%v)", cl, err)
	}
}

func TestBudgetSetFailsClosed(t *testing.T) {
	m := 300.0
	args := budgetSetArgs{book: "7", month: "2026-3", cate: "交通", money: &m}
	cases := []struct {
		name string
		f    *fakeQianji
	}{
		{"list error", &fakeQianji{listCode: 500}},
		{"missing list", &fakeQianji{budgets: `{}`}},
		{"null list", &fakeQianji{budgets: `{"list":null}`}},
		{"other period", &fakeQianji{budgets: `{"list":[{"type":1,"typeinfo":"2026,4","flag":1,"money":1}]}`}},
	}
	for _, c := range cases {
		_, cfg := c.f.server(t)
		if _, err := prepareBudgetSet(cfg, args, time.Now()); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}
}

func TestBudgetSetRejectsBadRequests(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	fake := &fakeQianji{budgets: fakeMarch}
	_, cfg := fake.server(t)
	cases := []struct {
		name string
		a    budgetSetArgs
	}{
		{"unknown book", budgetSetArgs{book: "404", month: "2026-3", total: f(1)}},
		{"book -1", budgetSetArgs{book: "-1", month: "2026-3", total: f(1)}},
		{"nothing to change", budgetSetArgs{book: "7", month: "2026-3"}},
		{"cate without money", budgetSetArgs{book: "7", month: "2026-3", cate: "交通"}},
		{"both edits", budgetSetArgs{book: "7", month: "2026-3", total: f(1), cate: "交通", money: f(1)}},
		{"negative", budgetSetArgs{book: "7", month: "2026-3", total: f(-1)}},
		{"no change", budgetSetArgs{book: "7", month: "2026-3", total: f(1000)}},
		{"remove the only budget", budgetSetArgs{book: "7", month: "2026-3", cate: "餐饮", money: f(0)}},
	}
	for _, c := range cases {
		a := c.a
		if c.name == "remove the only budget" {
			fake.budgets = `{"list":[{"type":1,"typeinfo":"2026,3","flag":2,"cateid":10,"money":5}]}`
		}
		if _, err := prepareBudgetSet(cfg, a, time.Now()); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}
	if len(fake.submitted) != 0 {
		t.Errorf("rejected requests must not submit: %v", fake.submitted)
	}
}

func Example_renderBudgets() {
	render := renderBudgets(budgetPeriod{2026, 3}, testCats)
	render(json.RawMessage(`{"list":[
		{"flag":1,"cateid":-1,"money":1000,"used":1100,"notbudget":30},
		{"flag":2,"cateid":10,"money":500,"used":100,"count":3},
		{"flag":2,"cateid":11,"money":200,"used":250,"count":2}
	]}`))
	render(json.RawMessage(`{"list":[{"flag":2,"cateid":20,"money":80,"used":10}]}`))
	render(json.RawMessage(`{"list":[]}`))
	// Output:
	// 2026-03 预算
	// 总预算                 1000.00  已用 1100.00  超支 100.00
	//   不计入预算 30.00
	// 餐饮                   500.00  已用 100.00  剩余 400.00  3 笔
	//   └ 外卖               200.00  已用 250.00  超支 50.00  2 笔
	// 2026-03 预算
	// 总预算（分类合计）             80.00  已用 10.00  剩余 70.00
	// 交通                    80.00  已用 10.00  剩余 70.00
	// 2026-03 预算
	// (当期未设预算)
}

func TestBudgetToolBookID(t *testing.T) {
	f := &fakeQianji{budgets: fakeMarch}
	_, cfg := f.server(t)
	// A real id works as a string (what the schema asks for) or a number.
	for _, id := range []any{"7", float64(7)} {
		a := map[string]any{"bookid": id, "month": "2026-3", "total": float64(2000)}
		if out, err := budgetSetToolRun(cfg, a); err != nil || !strings.Contains(out, `"dry_run":true`) {
			t.Errorf("bookid %v: out=%s err=%v", id, out, err)
		}
	}
	// -1 is rejected with the ids to use instead, in either form.
	for _, id := range []any{"-1", float64(-1)} {
		a := map[string]any{"bookid": id, "month": "2026-3", "total": float64(2000)}
		_, err := budgetSetToolRun(cfg, a)
		if err == nil || !strings.Contains(err.Error(), "b（id 7）") || !strings.Contains(err.Error(), "不支持 -1") {
			t.Errorf("bookid %v: err = %v", id, err)
		}
	}
}

func TestBookRangeNullConfig(t *testing.T) {
	f := &fakeQianji{budgets: fakeMarch}
	_, cfg := f.server(t)
	if rng, err := bookRange(cfg, "8"); err != nil || rng != "" {
		t.Errorf("range = %q, %v", rng, err)
	}
}

func TestBudgetToolPreviewsByDefault(t *testing.T) {
	f := &fakeQianji{budgets: fakeMarch}
	_, cfg := f.server(t)
	a := map[string]any{"bookid": float64(7), "month": "2026-3", "total": float64(2000)}
	out, err := budgetSetToolRun(cfg, a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dry_run":true`) || len(f.submitted) != 0 {
		t.Errorf("preview out=%s submitted=%d", out, len(f.submitted))
	}
	a["confirm"] = true
	if _, err := budgetSetToolRun(cfg, a); err != nil {
		t.Fatal(err)
	}
	if len(f.submitted) != 1 {
		t.Errorf("confirm submitted %d times", len(f.submitted))
	}
}
