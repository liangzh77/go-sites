package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/liangzh77/go-sites/internal/modelusage"
)

//go:embed usage_admin.html
var adminHTML string
var adminTemplate = template.Must(template.New("usage").Parse(adminHTML))

//go:embed usage_login.html
var loginHTML string
var loginTemplate = template.Must(template.New("login").Parse(loginHTML))

const adminCookieName = "__Secure-ai_demo_admin"
const adminSessionTTL = 8 * time.Hour

func usageLogin(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, message); err != nil {
		log.Printf("admin login render: %v", err)
	}
}

func recordModelSuccess(db *sql.DB) {
	if err := modelusage.Record(db, time.Now()); err != nil {
		log.Printf("model usage record failed: %v", err)
	}
}

type chartPoint struct {
	X, Y  int
	Label string
}
type adminView struct {
	Days                     []modelusage.Day
	Total, Today             int
	Points, XLabels, YLabels []chartPoint
	Line                     string
}

func usageView(days []modelusage.Day) adminView {
	view := adminView{Days: days}
	peak := 0
	for _, d := range days {
		peak = max(peak, d.Count)
		view.Total += d.Count
	}
	if len(days) != 0 {
		view.Today = days[len(days)-1].Count
	}
	step := max(1, (peak+3)/4)
	for i := 0; i <= 4; i++ {
		view.YLabels = append(view.YLabels, chartPoint{42, 184 - i*40, fmt.Sprint(i * step)})
	}
	var line []string
	for i, d := range days {
		x := 48 + i*552/max(1, len(days)-1)
		if len(days) == 1 {
			x = 324
		}
		y := 184 - d.Count*160/(4*step)
		view.Points = append(view.Points, chartPoint{x, y, fmt.Sprintf("%s：%d 次", d.Date, d.Count)})
		line = append(line, fmt.Sprintf("%d,%d", x, y))
		if i == 0 || i == len(days)-1 || (len(days) >= 6 && i%((len(days)+3)/4) == 0 && len(days)-1-i >= 3) {
			view.XLabels = append(view.XLabels, chartPoint{x, 207, d.Date[5:]})
		}
	}
	view.Line = strings.Join(line, " ")
	return view
}

func newUsageAdmin(db *sql.DB, password string) http.Handler {
	if password == "" {
		return http.NotFoundHandler()
	}
	want := sha256.Sum256([]byte(password))
	var mu sync.Mutex
	sessions := make(map[string]time.Time)
	cookieToken := func(r *http.Request) string {
		if c, err := r.Cookie(adminCookieName); err == nil {
			return c.Value
		}
		return ""
	}
	setSessionCookie := func(w http.ResponseWriter, token string, maxAge int) {
		http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: token, Path: "/ai-demo/admin/", MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}
	var blockedUntil time.Time
	var failures int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		now := time.Now()
		if r.Method == http.MethodPost {
			if r.Header.Get("Origin") != "https://"+r.Host {
				usageLogin(w, http.StatusForbidden, "请从管理后台页面重新提交。")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			if err := r.ParseForm(); err != nil {
				usageLogin(w, http.StatusBadRequest, "无法读取密码，请重试。")
				return
			}
			mu.Lock()
			if r.PostForm.Get("action") == "logout" {
				delete(sessions, cookieToken(r))
				mu.Unlock()
				setSessionCookie(w, "", -1)
				w.Header().Set("Location", "./")
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			if !now.Before(blockedUntil) {
				blockedUntil = now.Add(time.Minute)
				failures = 0
			}
			if failures >= 8 {
				mu.Unlock()
				w.Header().Set("Retry-After", "60")
				usageLogin(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试。")
				return
			}
			got := sha256.Sum256([]byte(r.PostForm.Get("password")))
			allowed := subtle.ConstantTimeCompare(got[:], want[:]) == 1
			if !allowed {
				failures++
				mu.Unlock()
				usageLogin(w, http.StatusUnauthorized, "密码不正确，请重试。")
				return
			}
			var raw [32]byte
			if _, err := rand.Read(raw[:]); err != nil {
				mu.Unlock()
				usageLogin(w, http.StatusServiceUnavailable, "暂时无法登录，请重试。")
				return
			}
			token := hex.EncodeToString(raw[:])
			delete(sessions, cookieToken(r))
			var oldest string
			for id, until := range sessions {
				if !until.After(now) {
					delete(sessions, id)
				} else if oldest == "" || until.Before(sessions[oldest]) {
					oldest = id
				}
			}
			if len(sessions) >= 128 {
				delete(sessions, oldest)
			}
			sessions[token] = now.Add(adminSessionTTL)
			mu.Unlock()
			setSessionCookie(w, token, int(adminSessionTTL.Seconds()))
			w.Header().Set("Location", "./") // Keep the public path across the proxy rewrite.
			w.WriteHeader(http.StatusSeeOther)
			return
		}
		mu.Lock()
		loggedIn := sessions[cookieToken(r)].After(now)
		mu.Unlock()
		if !loggedIn {
			usageLogin(w, http.StatusOK, "")
			return
		}
		days, err := modelusage.Days(r.Context(), db, now)
		if err != nil {
			log.Printf("model usage query failed: %v", err)
			http.Error(w, "统计暂时不可用", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := adminTemplate.Execute(w, usageView(days)); err != nil {
			log.Printf("model usage render: %v", err)
		}
	})
}
