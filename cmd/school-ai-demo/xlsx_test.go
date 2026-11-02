package main

import (
	"strings"
	"testing"
)

func TestXLSXRoundTrip(t *testing.T) {
	rows := [][]string{
		{"姓名", "性别", "家庭住址"},
		{"张三", "男", "河北省石家庄市长安区中山东路 1 号"},
		{"李四", "女", ""},
		{"王五 & 赵六", "<特殊字符>", "带\"引号\"的地址"},
	}
	data, err := writeXLSX(rows)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readXLSX(data)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Rows) != len(rows) {
		t.Fatalf("rows = %d, want %d (%v)", len(got.Rows), len(rows), got.Rows)
	}
	for i := range rows {
		for j := range rows[i] {
			if cellAt(got.Rows[i], j) != rows[i][j] {
				t.Errorf("cell %d,%d = %q, want %q", i, j, cellAt(got.Rows[i], j), rows[i][j])
			}
		}
	}
}

func TestSheetTextIsHeaderLabeled(t *testing.T) {
	s := sheet{Name: "学生名单", Rows: [][]string{
		{"姓名", "性别", "出生年月"},
		{"张三", "男", "2019年3月"},
		{"", "", ""},
	}}
	text := s.text()
	if !strings.Contains(text, "姓名: 张三") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "出生年月: 2019年3月") {
		t.Fatalf("text = %q", text)
	}
}

func TestSheetTextReadsALabelValueForm(t *testing.T) {
	// How schools usually hand over one child's details: label, value, one
	// per row. Read as a table this is a header of nonsense and one record.
	s := sheet{Name: "登记", Rows: [][]string{
		{"姓名", "张三"},
		{"性别", "男"},
		{"出生年月", "2019年3月"},
		{"家庭住址", "河北省石家庄市长安区"},
		{"联系人及电话", ""},
	}}
	text := s.text()
	for _, want := range []string{"姓名：张三", "性别：男", "出生年月：2019年3月", "家庭住址：河北省石家庄市长安区"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q; got:\n%s", want, text)
		}
	}
	// A narrow sheet is genuinely ambiguous, so both readings are sent and
	// labelled; guessing one would risk silently dropping every value.
	if !strings.Contains(text, "按登记表读") || !strings.Contains(text, "按两列表格读") {
		t.Errorf("both readings should be labelled:\n%s", text)
	}
}

func TestWideSheetIsNotOfferedTwoReadings(t *testing.T) {
	s := sheet{Rows: [][]string{
		{"姓名", "性别", "出生年月", "民族"},
		{"张三", "男", "2019年3月", "汉族"},
		{"李四", "女", "2020年1月", "回族"},
	}}
	text := s.text()
	if strings.Contains(text, "按登记表读") {
		t.Fatalf("a four-column sheet is a table, not a form:\n%s", text)
	}
	if !strings.Contains(text, "民族: 汉族") {
		t.Fatalf("text = %q", text)
	}
}

func TestSheetTextWithNoHeader(t *testing.T) {
	s := sheet{Rows: [][]string{{"张三", "男", "9岁"}}}
	text := s.text()
	if !strings.Contains(text, "第1列: 张三") {
		t.Fatalf("text = %q", text)
	}
}

func TestSheetTextSkipsAPreamble(t *testing.T) {
	// Real exports often start with a title row and a blank row.
	s := sheet{Rows: [][]string{
		{"2026级新生信息表"},
		{"姓名", "性别"},
		{"张三", "男"},
	}}
	text := s.text()
	if !strings.Contains(text, "姓名: 张三") {
		t.Fatalf("expected the header row to be found; got %q", text)
	}
}

func TestReadXLSXRejectsGarbage(t *testing.T) {
	if _, err := readXLSX([]byte("this is not a zip")); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "Excel") {
		t.Fatalf("error should mention Excel: %v", err)
	}
}

func TestWriteXLSXEscapesXML(t *testing.T) {
	data, err := writeXLSX([][]string{{`a & b <c> "d"`}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readXLSX(data)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cellAt(got.Rows[0], 0) != `a & b <c> "d"` {
		t.Fatalf("round trip lost characters: %q", cellAt(got.Rows[0], 0))
	}
}

func TestColumnName(t *testing.T) {
	cases := map[int]string{0: "A", 1: "B", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA"}
	for i, want := range cases {
		if got := columnName(i); got != want {
			t.Errorf("columnName(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestCSVToTextMatchesTheSheetFormat(t *testing.T) {
	text := csvToText("姓名,性别\n张三,男\n李四,\"女, 10岁\"\n")
	for _, want := range []string{"姓名: 张三", "性别: 男", "性别: 女, 10岁"} {
		if !strings.Contains(text, want) {
			t.Errorf("csv text missing %q; got:\n%s", want, text)
		}
	}
}
