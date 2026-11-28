// Package modelusage stores daily counts of successful inference responses only.
package modelusage

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Shanghai stays at UTC+8; this ledger only tracks dates from its installation.
var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

func day(t time.Time) string { return t.In(shanghai).Format(time.DateOnly) }

func Open(dataDir string, now time.Time) (*sql.DB, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "model-usage.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS successes(day TEXT PRIMARY KEY, n INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS tracking(id INTEGER PRIMARY KEY CHECK(id=1), start_day TEXT NOT NULL);`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO tracking(id,start_day) VALUES(1,?)`, day(now)); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Record increments the day only after a usable inference response.
func Record(db *sql.DB, now time.Time) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`INSERT INTO successes(day,n) VALUES(?,1) ON CONFLICT(day) DO UPDATE SET n=n+1`, day(now))
	return err
}

type Day struct {
	Date  string
	Count int
}

// Days returns up to the latest 30 days, including zeroes only after tracking starts.
func Days(ctx context.Context, db *sql.DB, now time.Time) ([]Day, error) {
	var start string
	if err := db.QueryRowContext(ctx, `SELECT start_day FROM tracking WHERE id=1`).Scan(&start); err != nil {
		return nil, err
	}
	end := now.In(shanghai)
	first := end.AddDate(0, 0, -29)
	if day(first) < start {
		var err error
		first, err = time.ParseInLocation(time.DateOnly, start, shanghai)
		if err != nil {
			return nil, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT day,n FROM successes WHERE day BETWEEN ? AND ?`, day(first), day(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return nil, err
		}
		counts[d.Date] = d.Count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []Day
	for t := first; !t.After(end); t = t.AddDate(0, 0, 1) {
		d := day(t)
		out = append(out, Day{d, counts[d]})
	}
	return out, nil
}
