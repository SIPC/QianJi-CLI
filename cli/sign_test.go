package main

import (
	"strconv"
	"testing"
)

func TestMD5Hex(t *testing.T) {
	// standard MD5 vectors (lowercase hex, as za.i.b produces)
	cases := map[string]string{
		"":    "d41d8cd98f00b204e9800998ecf8427e",
		"abc": "900150983cd24fb0d6963f7d28e17f72",
	}
	for in, want := range cases {
		if got := md5hex(in); got != want {
			t.Errorf("md5hex(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestSignGolden pins the recovered scheme to values computed by an independent
// implementation (see docs/qianji-api.md). If this breaks, the signing constants
// or concatenation order changed.
func TestSignGolden(t *testing.T) {
	h := signHeadersAt("bill", "syncall", 1700000000000)
	wantReqid := "e5b36cd3bb539dc0c6b725a3b15e1acb"
	wantTok := "4d27a75e2c74e8988799e449279b55b5"
	if h["reqidv2"] != wantReqid {
		t.Errorf("reqidv2 = %s, want %s", h["reqidv2"], wantReqid)
	}
	if h["tok"] != wantTok {
		t.Errorf("tok = %s, want %s", h["tok"], wantTok)
	}
}

// TestSignStructure verifies tok is derived from reqid exactly as the server checks it,
// for an arbitrary time (reqid is an opaque nonce to the server; tok binds it).
func TestSignStructure(t *testing.T) {
	ctrl, act := "book", "list"
	h := signHeadersAt(ctrl, act, 123456789)
	reqid := h["reqidv2"]
	enc := md5hex(reqid + saltEnc)
	wantTok := md5hex(reqid + tokConst + ctrl + enc + act)
	if h["tok"] != wantTok {
		t.Errorf("tok not consistent with reqid: got %s want %s", h["tok"], wantTok)
	}
	if len(reqid) != 32 {
		t.Errorf("reqid should be 32 hex chars, got %d", len(reqid))
	}
}

func TestItoa(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 9, 10, 1234, 1700000009015591, -1700000009015591} {
		if got, want := itoa(v), strconv.FormatInt(v, 10); got != want {
			t.Errorf("itoa(%d) = %s, want %s", v, got, want)
		}
	}
}
