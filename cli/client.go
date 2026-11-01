package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultHost = "https://api.qianjiapp.com/"

// Client performs signed requests against the Qianji private API.
type Client struct {
	cfg  *Config
	http *http.Client
}

func newClient(cfg *Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) host() string {
	if c.cfg.Host != "" {
		return c.cfg.Host
	}
	return defaultHost
}

// Envelope is the standard response wrapper: {"ec":200,"em":"","data":{...}}.
type Envelope struct {
	EC   int             `json:"ec"`
	EM   string          `json:"em"`
	Data json.RawMessage `json:"data"`
}

// APIError is returned when the server responds with ec != 200.
type APIError struct {
	EC int
	EM string
}

func (e *APIError) Error() string {
	if e.EM != "" {
		return fmt.Sprintf("api error %d: %s", e.EC, e.EM)
	}
	return fmt.Sprintf("api error %d", e.EC)
}

// call issues a signed POST to <host><ctrl>/<act> with form params and returns the parsed envelope.
func (c *Client) call(ctrl, act string, params map[string]string) (*Envelope, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	// uid + fr are attached by the app to authenticated calls.
	if c.cfg.UID != "" {
		if _, ok := params["uid"]; !ok {
			form.Set("uid", c.cfg.UID)
		}
		form.Set("fr", c.cfg.UID)
	}

	endpoint := c.host() + ctrl + "/" + act
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range c.deviceHeaders(ctrl, act) {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(body), 300))
	}
	if env.EC != 200 {
		return nil, &APIError{EC: env.EC, EM: env.EM}
	}
	return &env, nil
}

// deviceHeaders builds the full header set: signing + device/version + auth token.
func (c *Client) deviceHeaders(ctrl, act string) map[string]string {
	d := c.cfg.Device.withDefaults()
	h := map[string]string{
		"ctrl":           ctrl,
		"act":            act,
		"mk":             d.Market,
		"os":             "1",
		"osvs":           d.OSVer,
		"devbrand":       d.Brand,
		"devname":        d.Name,
		"devid":          ensureDevID(c.cfg),
		"vs":             itoa(versionCode),
		"pkg":            pkg,
		"vsn":            versionName,
		"timezoneoffset": d.TZOffset,
		"clang":          d.Lang,
		"cregion":        d.Region,
	}
	for k, v := range signHeaders(ctrl, act) {
		h[k] = v
	}
	if c.cfg.Token != "" {
		h["utoken"] = c.cfg.Token
		h["htoken"] = "1"
	}
	return h
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
