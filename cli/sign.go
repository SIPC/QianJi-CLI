package main

import (
	"crypto/md5"
	"encoding/hex"
	"time"
)

// Request-signing scheme recovered from libfabricsuffer.so (com.mutangtech.qianji 4.5.4).
// Everything is plain MD5 over concatenated strings; the two native entry points
// (JNIHelper.requestId / encreqid) and the Java helper za.i.b reduce to:
//
//	reqid  = md5( pkg + str(nowMillis + versionCode + magic) + ctrl + act + saltReq )
//	encreq = md5( reqid + saltEnc )
//	tok    = md5( reqid + tokConst + ctrl + encreq + act )
//
// The server only needs `tok` to agree with `reqid`, `ctrl`, `act`; the time/pkg/vs
// baked into reqid are not re-derived server-side, so reqid acts as an opaque nonce.
const (
	pkg      = "com.mutangtech.qianji"
	versionCode = 1234
	versionName = "4.5.4"
	saltReq  = "free20170908&x_*1127"
	saltEnc  = "michaeljackson"
	tokConst = "1172020"
	magic    = 9015591
)

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// signHeaders returns the per-request signing headers for a given controller/action.
func signHeaders(ctrl, act string) map[string]string {
	return signHeadersAt(ctrl, act, time.Now().UnixMilli())
}

// signHeadersAt is the deterministic core of signHeaders (nowMillis injected for tests).
func signHeadersAt(ctrl, act string, nowMillis int64) map[string]string {
	t := nowMillis + versionCode + magic
	reqid := md5hex(pkg + itoa(t) + ctrl + act + saltReq)
	encreq := md5hex(reqid + saltEnc)
	tok := md5hex(reqid + tokConst + ctrl + encreq + act)
	return map[string]string{
		"reqidv2": reqid,
		"tok":     tok,
	}
}

func itoa(v int64) string {
	// small, allocation-free int64 -> decimal (matches native "%ld")
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var buf [20]byte
	i := len(buf)
	u := v
	if neg {
		u = -u
	}
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
