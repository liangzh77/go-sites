package main

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func loadTemplate(t *testing.T, slug string) *parsedTemplate {
	t.Helper()
	raw, err := templateFS.ReadFile("templates/" + slug + ".html")
	if err != nil {
		t.Fatalf("read %s: %v", slug, err)
	}
	tpl, err := parseTemplateHTML(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", slug, err)
	}
	return tpl
}

func fieldByLabel(t *testing.T, tpl *parsedTemplate, label string) field {
	t.Helper()
	for _, f := range tpl.Fields {
		if f.Label == label {
			return f
		}
	}
	var got []string
	for _, f := range tpl.Fields {
		got = append(got, f.Label)
	}
	t.Fatalf("no field labelled %q in %s; have: %s", label, tpl.Name, strings.Join(got, ", "))
	return field{}
}

func TestParseEnrollmentTemplate(t *testing.T) {
	tpl := loadTemplate(t, "enrollment")

	if tpl.Name != "学生入学登记表" {
		t.Fatalf("name = %q", tpl.Name)
	}
	// 3 on the metadata line (one of them a span inside a caption cell) +
	// 11 personal (photo position included) + 4 biography + 28 family + 5
	// situation.
	if len(tpl.Fields) != 51 {
		t.Errorf("fields = %d, want 51", len(tpl.Fields))
	}

	cases := []struct {
		label   string
		section string
		hint    string
	}{
		{"登记姓名", "登记信息", "姓名"},
		{"学号", "登记信息", "学号"},
		{"班主任", "登记信息", "班主任"},
		{"姓名", "个人基本信息", "姓 名"},
		{"详细家庭住址", "个人基本信息", "家庭住址（详细）"},
		{"小学担任职务", "本人简历", "小学担任过何职务"},
		{"特长", "本人简历", "特长（必须写）"},
		{"家庭成员1姓名", "家庭成员基本情况", "姓名"},
		{"家庭成员4手机号码2", "家庭成员基本情况", "手机号码2"},
		{"家庭主要经济来源", "家庭情况介绍", "家庭主要经济来源"},
		{"父母对学习的期望", "家庭情况介绍", "父母对你学习的期望是什么？"},
	}
	for _, c := range cases {
		f := fieldByLabel(t, tpl, c.label)
		if f.Section != c.section {
			t.Errorf("%s section = %q, want %q", c.label, f.Section, c.section)
		}
		if !strings.Contains(f.Hint, c.hint) {
			t.Errorf("%s hint = %q, want it to contain %q", c.label, f.Hint, c.hint)
		}
		if f.Kind != kindText {
			t.Errorf("%s kind = %q, want text", c.label, f.Kind)
		}
	}

	// Every family row has to exist: mixing row 3's phone into row 2 would be
	// worse than leaving it blank.
	for row := 1; row <= 4; row++ {
		for _, col := range []string{"姓名", "关系", "残疾证情况", "工作单位", "家庭电话", "手机号码1", "手机号码2"} {
			fieldByLabel(t, tpl, "家庭成员"+strconv.Itoa(row)+col)
		}
	}
}

func TestEnrollmentHTMLKeepsItsLayout(t *testing.T) {
	tpl := loadTemplate(t, "enrollment")

	if strings.Contains(tpl.HTML, slotMarker) {
		t.Fatalf("parse-time marker %q leaked into the stored html", slotMarker)
	}
	for _, f := range tpl.Fields {
		if !strings.Contains(tpl.HTML, `data-field-id="`+f.ID+`"`) {
			t.Fatalf("field %s (%s) has no anchor in the html", f.ID, f.Label)
		}
	}
	// The original stylesheet and structure are the form: losing them would
	// break printing, which is how these forms actually get used.
	for _, want := range []string{
		"@page", "@media print", "#06235d", "794px",
		`class="page"`, "rowspan=\"3\"", "colspan=\"7\"",
	} {
		if !strings.Contains(tpl.HTML, want) {
			t.Errorf("html lost %q", want)
		}
	}
}

func TestParseCollectionTemplate(t *testing.T) {
	tpl := loadTemplate(t, "collection")

	if tpl.Name != "学生信息采集登记表" {
		t.Fatalf("name = %q", tpl.Name)
	}
	if len(tpl.Fields) != 73 {
		t.Errorf("fields = %d, want 73", len(tpl.Fields))
	}

	// A slot on the caption line outside the grid still counts.
	date := fieldByLabel(t, tpl, "登记日期")
	if date.Kind != kindText || date.Section != "学生信息采集登记表" {
		t.Errorf("登记日期 = %+v", date)
	}

	// Radio groups are fields too: they are not contenteditable, but the model
	// still has to be able to answer "是".
	only := fieldByLabel(t, tpl, "是否独子")
	if only.Kind != kindChoice {
		t.Fatalf("是否独子 kind = %q, want choice", only.Kind)
	}
	if strings.Join(only.Options, "/") != "是/否" {
		t.Errorf("是否独子 options = %v", only.Options)
	}
	if got := strings.Join(fieldByLabel(t, tpl, "户籍性质").Options, "/"); got != "城镇/农村" {
		t.Errorf("户籍性质 options = %s", got)
	}
	if got := strings.Join(fieldByLabel(t, tpl, "学生类别").Options, "/"); got != "普通学生/随班就读学生" {
		t.Errorf("学生类别 options = %s", got)
	}
	if !strings.Contains(tpl.HTML, `data-field-kind="choice"`) {
		t.Error("no choice slot marked in the html")
	}

	seen := map[string]bool{}
	for _, f := range tpl.Fields {
		if seen[f.ID] {
			t.Fatalf("duplicate field id %s", f.ID)
		}
		seen[f.ID] = true
	}
}

func TestCollectionSectionsFollowTheRowGroups(t *testing.T) {
	tpl := loadTemplate(t, "collection")

	// The three row-group blocks each get their own section, and the two
	// side-by-side groups in one row are told apart.
	cases := []struct{ label, section string }{
		{"家庭成员1姓名", "家庭成员信息登记"},
		{"家庭成员3联系电话", "家庭成员信息登记"},
		{"曾就读学校1名称", "曾就读学校信息"},
		{"曾就读学校3证明人", "曾就读学校信息"},
		{"兴趣爱好第一行", "兴趣爱好"},
		{"个人特长第一行", "个人特长"},
		{"兴趣爱好第二行", "兴趣爱好"},
		{"个人特长第二行", "个人特长"},
		// Rows after the last group belong to the table, not to 个人特长.
		{"经常接送人", "学生信息采集登记表"},
		{"备注", "学生信息采集登记表"},
	}
	for _, c := range cases {
		if got := fieldByLabel(t, tpl, c.label).Section; got != c.section {
			t.Errorf("%s section = %q, want %q", c.label, got, c.section)
		}
	}
}

func TestColumnCaptionsDoNotBleedBetweenBlocks(t *testing.T) {
	tpl := loadTemplate(t, "collection")

	// 起止年月 belongs to the 曾就读学校 block. A field under it must not be
	// hinted with it once the block ends, or the model sends a date to a name.
	for _, label := range []string{"现就读班级", "入学年月", "班主任", "疾病史", "备注"} {
		if hint := fieldByLabel(t, tpl, label).Hint; strings.Contains(hint, "起止年月") ||
			strings.Contains(hint, "学校名称") || strings.Contains(hint, "证明人") {
			t.Errorf("%s hint = %q still carries the 曾就读学校 captions", label, hint)
		}
	}
	if hint := fieldByLabel(t, tpl, "曾就读学校2名称").Hint; !strings.Contains(hint, "学校名称") {
		t.Errorf("曾就读学校2名称 hint = %q, want the 学校名称 caption", hint)
	}
}

func TestHintsDisambiguateRepeatedCaptions(t *testing.T) {
	tpl := loadTemplate(t, "collection")

	var names []field
	for _, f := range tpl.Fields {
		if strings.HasPrefix(f.Label, "家庭成员") && strings.HasSuffix(f.Label, "姓名") {
			names = append(names, f)
		}
	}
	if len(names) != 3 {
		t.Fatalf("family name fields = %d, want 3", len(names))
	}
	for _, f := range names {
		if f.Hint == "" {
			t.Errorf("%s has no hint", f.Label)
		}
		if f.Section != "家庭成员信息登记" {
			t.Errorf("%s section = %q", f.Label, f.Section)
		}
	}
}

func TestTextOfDropsLineBreaksInsideACaption(t *testing.T) {
	// "家庭住址<br>（详细）" is one caption; a space in the middle would make
	// the model read it as two different things.
	if got := textOf(mustParseFragment(t, `<td>家庭住址<br>（详细）</td>`)); got != "家庭住址（详细）" {
		t.Fatalf("textOf = %q", got)
	}
}

func TestParseTemplateHTMLRejectsNothingButReports(t *testing.T) {
	// A document with no slots is not an error at parse time; the service
	// still refuses to serve it as a template because there is nothing to fill.
	tpl, err := parseTemplateHTML([]byte(`<html><head><title>空表</title></head><body><p>hi</p></body></html>`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tpl.Fields) != 0 {
		t.Fatalf("fields = %d, want 0", len(tpl.Fields))
	}
	if tpl.Name != "空表" {
		t.Fatalf("name = %q", tpl.Name)
	}
}

func mustParseFragment(t *testing.T, fragment string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader("<table><tr>" + fragment + "</tr></table>"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	node := findFirst(doc, "td")
	if node == nil {
		t.Fatal("no td in parsed fragment")
	}
	return node
}

func TestPlaceholderTextIsMarkedNotTreatedAsAValue(t *testing.T) {
	tpl := loadTemplate(t, "enrollment")
	// The photo slot is the one slot whose HTML already has text in it.
	if !strings.Contains(tpl.HTML, `data-placeholder="照片"`) {
		t.Fatal("the photo cell's placeholder text was not recorded")
	}
}
