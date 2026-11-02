package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The AI fill costs money per call, so it is capped per calendar day. The
// counter lives in SQLite rather than in memory: a restart must not hand out a
// fresh budget, and two service processes would otherwise each get their own.

const (
	dailyLimitEnv     = "SCHOOL_AI_DAILY_LIMIT"
	defaultDailyLimit = 100
)

// quotaZone is the family's day, not the server's: the budget resets at
// midnight in Shanghai even though the host clock runs on UTC.
func quotaZone() *time.Location { return shanghai() }

// dailyLimitFromEnv reads SCHOOL_AI_DAILY_LIMIT, treating 0 as unlimited and a
// missing or broken value as the default.
func dailyLimitFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(dailyLimitEnv))
	if raw == "" {
		return defaultDailyLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultDailyLimit
	}
	return n
}

// quotaDay is the calendar key a fill is counted against, e.g. 2026-11-02 in
// Shanghai. Counting on this string instead of a timestamp is what makes the
// reset at local midnight fall out for free.
func quotaDay(t time.Time) string {
	return t.In(quotaZone()).Format("2006-01-02")
}

// nextReset is the moment the current budget is replaced, for the message the
// user sees.
func nextReset(t time.Time) time.Time {
	local := t.In(quotaZone())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, quotaZone()).AddDate(0, 0, 1)
}

// quotaState is what the page shows: how much of today's budget is left.
type quotaState struct {
	Day       string    `json:"day"`
	Used      int       `json:"used"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Unlimited bool      `json:"unlimited"`
	ResetsAt  time.Time `json:"resetsAt"`
}

var errQuotaExceeded = errors.New("今日 AI 填表次数已用完")

// reserveQuota claims one call for the given day, or reports that the budget
// is spent. The check and the increment are one statement, so two requests
// racing on the last remaining call cannot both succeed: the WHERE clause
// fails for the loser and RowsAffected comes back zero.
func (s *store) reserveQuota(day string, limit int) (bool, int, error) {
	if limit <= 0 {
		return true, 0, nil // unlimited; nothing to count
	}
	res, err := s.db.Exec(`
		INSERT INTO quota_days (day, used, updated_at)
		VALUES (?, 1, ?)
		ON CONFLICT(day) DO UPDATE SET used = used + 1, updated_at = excluded.updated_at
		WHERE used < ?`,
		day, time.Now().UTC().Format(time.RFC3339), limit)
	if err != nil {
		return false, 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, 0, err
	}
	if n == 0 {
		// The WHERE clause refused the increment: the day is already full.
		used, _ := s.quotaUsed(day)
		return false, used, nil
	}
	used, err := s.quotaUsed(day)
	return true, used, err
}

// releaseQuota gives back a reservation whose model call never happened, so a
// network blip does not eat into a small daily budget.
func (s *store) releaseQuota(day string) {
	_, _ = s.db.Exec(`UPDATE quota_days SET used = used - 1 WHERE day = ? AND used > 0`, day)
}

func (s *store) quotaUsed(day string) (int, error) {
	var used int
	err := s.db.QueryRow(`SELECT used FROM quota_days WHERE day = ?`, day).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

// quota reports today's usage for the UI and the health endpoint.
func (s *store) quota(now time.Time, limit int) (quotaState, error) {
	day := quotaDay(now)
	used, err := s.quotaUsed(day)
	if err != nil {
		return quotaState{}, err
	}
	st := quotaState{
		Day:       day,
		Used:      used,
		Limit:     limit,
		Unlimited: limit <= 0,
		ResetsAt:  nextReset(now),
	}
	if limit > 0 {
		st.Remaining = limit - used
		if st.Remaining < 0 {
			st.Remaining = 0
		}
	}
	return st, nil
}

func quotaExceededMessage(limit int) string {
	return fmt.Sprintf("今日 AI 填表次数已用完（%d/%d），明天 00:00 重置", limit, limit)
}
