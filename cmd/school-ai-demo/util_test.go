package main

import (
	"testing"
	"time"
)

func TestURLEscapeKeepsUnreservedAndEncodesTheRest(t *testing.T) {
	cases := map[string]string{
		"form.xlsx":    "form.xlsx",
		"学生入学登记表.xlsx": "%E5%AD%A6%E7%94%9F%E5%85%A5%E5%AD%A6%E7%99%BB%E8%AE%B0%E8%A1%A8.xlsx",
		"a b.xlsx":     "a%20b.xlsx",
		"a+b&c.xlsx":   "a%2Bb%26c.xlsx",
	}
	for in, want := range cases {
		if got := urlEscape(in); got != want {
			t.Errorf("urlEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShanghaiIsUTCPlusEight(t *testing.T) {
	_, off := time.Now().In(shanghai()).Zone()
	if off != 8*3600 {
		t.Fatalf("offset = %d, want 8h", off)
	}
}
