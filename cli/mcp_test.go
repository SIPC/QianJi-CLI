package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBillsToolAddsImageURLs(t *testing.T) {
	t.Setenv("QIANJI_CONFIG_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/syncv2/pull" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ec":200,"em":"","data":{"changes":[
			{"id":1234567890123456789,"time":200,"money":5,"images":["_a","_b"]},
			{"id":1234567890123456790,"time":100,"money":6,"images":[]},
			{"id":1234567890123456791,"time":50,"money":7}
		],"hasmore":0}}`))
	}))
	defer srv.Close()
	cfg := &Config{Host: srv.URL + "/", UID: "u1", Token: "tok", DevID: "dev"}

	out, err := billsToolRun(cfg, map[string]any{"bookid": "7"})
	if err != nil {
		t.Fatal(err)
	}
	var bills []struct {
		ID        json.Number `json:"id"`
		Images    []string    `json:"images"`
		ImageURLs []string    `json:"image_urls"`
	}
	if err := json.Unmarshal([]byte(out), &bills); err != nil {
		t.Fatalf("%v in %s", err, out)
	}
	if len(bills) != 3 {
		t.Fatalf("got %d bills: %s", len(bills), out)
	}
	want := []string{billImageHost + "_a!webporigin", billImageHost + "_b!webporigin"}
	if strings.Join(bills[0].ImageURLs, " ") != strings.Join(want, " ") {
		t.Errorf("image_urls = %v, want %v", bills[0].ImageURLs, want)
	}
	if bills[0].ID.String() != "1234567890123456789" {
		t.Errorf("id lost precision: %s", bills[0].ID)
	}
	if len(bills[0].Images) != 2 {
		t.Errorf("images dropped: %v", bills[0].Images)
	}
	for _, b := range bills[1:] {
		if b.ImageURLs != nil {
			t.Errorf("bill %s has image_urls %v", b.ID, b.ImageURLs)
		}
	}
}

func TestWithImageURLsKeepsUnexpectedImages(t *testing.T) {
	raw := json.RawMessage(`{"id":1,"images":"_a"}`)
	if got := string(withImageURLs(raw)); got != string(raw) {
		t.Errorf("got %s", got)
	}
}
