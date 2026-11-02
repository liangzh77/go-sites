package main

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// shanghai is the timezone the school and its forms live in, so a downloaded
// form is named for the day the user actually filled it.
func shanghai() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

// urlEscape percent-encodes a filename for the RFC 5987 filename* parameter.
// QueryEscape would turn spaces into "+", which a browser then saves literally.
func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%")
		b.WriteString(upperHex(c >> 4))
		b.WriteString(upperHex(c & 0x0f))
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-', c == '.', c == '_', c == '~':
		return true
	}
	return false
}

func upperHex(n byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[n&0x0f]})
}

// newUUID returns a random RFC 4122 version 4 UUID. The OpenCode gateway wants
// a per-conversation id in x-opencode-session, and any random UUID is fine.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A weaker fallback still satisfies "distinct per request"; the gateway
		// only uses it to group calls.
		return fmt.Sprintf("%d-0000-4000-8000-000000000000", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
