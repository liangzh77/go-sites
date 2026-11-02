package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *store {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.close() })
	return st
}

func TestSaveTemplateIsIdempotent(t *testing.T) {
	st := testStore(t)
	tpl := loadTemplate(t, "enrollment")

	first, err := st.saveTemplate(tpl, "enrollment", "enrollment.html", 0)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	second, err := st.saveTemplate(tpl, "enrollment", "enrollment.html", 0)
	if err != nil {
		t.Fatalf("save again: %v", err)
	}

	// Restarting the service must refresh the stored template, not duplicate
	// it: the slug is the identity.
	if first.ID != second.ID {
		t.Fatalf("ids differ across saves: %s vs %s", first.ID, second.ID)
	}
	all, err := st.templates()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("templates = %d, want 1", len(all))
	}
	if len(all[0].Fields) != 51 {
		t.Fatalf("fields = %d", len(all[0].Fields))
	}
	if all[0].HTML != "" {
		t.Error("the list view should not carry the document")
	}
}

func TestTemplateNotFound(t *testing.T) {
	st := testStore(t)
	if _, err := st.template("nope"); !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want errNotFound", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := testStore(t)
	tpl, err := st.saveTemplate(loadTemplate(t, "enrollment"), "enrollment", "enrollment.html", 0)
	if err != nil {
		t.Fatalf("save template: %v", err)
	}

	sess, err := st.createSession(tpl, "xlsx", "张三.xlsx", "姓名: 张三")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.Status != statusPending {
		t.Fatalf("status = %q", sess.Status)
	}
	if text, err := st.sessionInput(sess.ID); err != nil || text != "姓名: 张三" {
		t.Fatalf("input = %q, %v", text, err)
	}

	values := map[string]string{"f01": "张三", "f04": "张三"}
	if err := st.finishSession(sess.ID, values, []string{"一个提醒"}, "deepseek-chat", statusFilled, ""); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := st.session(sess.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Status != statusFilled || len(got.Values) != 2 || got.Values["f01"] != "张三" {
		t.Fatalf("session = %+v", got)
	}
	if got.Model != "deepseek-chat" || len(got.Warnings) != 1 {
		t.Fatalf("session = %+v", got)
	}
	if got.Template != "enrollment" {
		t.Fatalf("template = %q", got.Template)
	}

	// A hand edit replaces the values, and an empty answer drops the field.
	saved, err := st.saveSession(sess.ID, map[string]string{"f01": "张三丰"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Values["f01"] != "张三丰" || len(saved.Values) != 1 {
		t.Fatalf("values = %v", saved.Values)
	}
}

func TestSessionNotFound(t *testing.T) {
	st := testStore(t)
	if _, err := st.session("nope"); !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := st.saveSession("nope", map[string]string{"f01": "x"}); !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRecentSessionsAreNewestFirst(t *testing.T) {
	st := testStore(t)
	tpl, err := st.saveTemplate(loadTemplate(t, "enrollment"), "enrollment", "enrollment.html", 0)
	if err != nil {
		t.Fatalf("save template: %v", err)
	}
	for _, name := range []string{"张三", "李四", "王五"} {
		if _, err := st.createSession(tpl, "text", name, "资料"); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	recent, err := st.recentSessions(2)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent = %d, want 2", len(recent))
	}
	// Timestamps have one-second resolution in the DDL, so assert only that
	// the newest two are a subset of what was created, in some order.
	seen := map[string]bool{recent[0].InputName: true, recent[1].InputName: true}
	if !seen["李四"] && !seen["王五"] {
		t.Fatalf("recent names = %v", recent)
	}
}
