package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
	// The app's Volley HurlStack uses HttpURLConnection (HTTP/1.1). Go defaults
	// to HTTP/2 over TLS; force HTTP/1.1 to match the app's transport.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ForceAttemptHTTP2 = false
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second, Transport: tr}}
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
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", c.userAgent())
	// The app sends header names in lowercase (addHeader). Go's Header.Set
	// canonicalizes to Title-Case; assign the map directly to keep them lowercase.
	for k, v := range c.deviceHeaders(ctrl, act) {
		req.Header[k] = []string{v}
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
	// vs/vsn default to the reverse-engineered version but can be overridden
	// (env wins over config) so the client can be bumped without recompiling
	// when the server raises its minimum-version gate.
	vs := firstNonEmpty(os.Getenv("QIANJI_VS"), c.cfg.VS, itoa(versionCode))
	vsn := firstNonEmpty(os.Getenv("QIANJI_VSN"), c.cfg.VSN, versionName)
	h := map[string]string{
		"ctrl":           ctrl,
		"act":            act,
		"mk":             d.Market,
		"os":             "1",
		"osvs":           d.OSVer,
		"devbrand":       d.Brand,
		"devname":        d.Name,
		"devid":          ensureDevID(c.cfg),
		"vs":             vs,
		"pkg":            pkg,
		"vsn":            vsn,
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

// userAgent mimics the app's request fingerprint. Volley's HurlStack sets no
// explicit User-Agent, so the app sends Android's default HttpURLConnection
// agent (Dalvik/...); we reproduce that shape. Override via $QIANJI_UA or config.
func (c *Client) userAgent() string {
	if v := os.Getenv("QIANJI_UA"); v != "" {
		return v
	}
	if c.cfg.UA != "" {
		return c.cfg.UA
	}
	d := c.cfg.Device.withDefaults()
	return "Dalvik/2.1.0 (Linux; U; Android 15; " + d.Name + " Build/AQ3A.240912.001)"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
