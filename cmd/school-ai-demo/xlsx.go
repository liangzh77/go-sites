package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The demo has to read whatever Excel the parent happens to have, and the
// server build runs with a short deadline, so the reader is a small targeted
// one for .xlsx (a zip of XML) rather than a full spreadsheet engine.

const (
	maxSheetCells = 20000
	maxSheetRows  = 2000
)

// sheet is one decoded worksheet: rows of plain strings.
type sheet struct {
	Name string
	Rows [][]string
}

// text flattens a sheet into lines the model can read.
//
// Two shapes turn up in practice and they need opposite readings. A wide sheet
// is a table: the first row names the columns, each later row is one record, so
// every cell reads best as "列名: 值". A narrow sheet may instead be a form —
// label in column A, value in column B, one field per row — which, read as a
// table, is a header of nonsense plus a single record. Rather than guess and
// sometimes guess wrong, a narrow sheet is sent both ways and labelled; the
// model is good at picking the reading that makes sense, and a wrong guess here
// would silently lose every value.
func (s sheet) text() string {
	rows := trimTrailingEmptyRows(s.Rows)
	if len(rows) == 0 {
		return ""
	}
	if widestRow(rows) <= 2 && len(rows) >= 3 {
		return s.bothReadings(rows)
	}
	return tableReading(rows)
}

// bothReadings spells out a narrow sheet as a form and as a two-column table.
func (s sheet) bothReadings(rows [][]string) string {
	var form, table []string
	for _, row := range rows {
		label, value := cellAt(row, 0), cellAt(row, 1)
		switch {
		case label == "" && value == "":
			continue
		case value == "":
			form = append(form, "【"+label+"】")
		default:
			form = append(form, label+"："+value)
		}
	}
	table = append(table, tableReading(rows))

	var b strings.Builder
	b.WriteString("（这张表可能是「一行一个项目」的登记表，也可能是两列表格，两种读法都给出）\n")
	b.WriteString("按登记表读：\n")
	b.WriteString(strings.Join(form, "\n"))
	b.WriteString("\n按两列表格读：\n")
	b.WriteString(strings.Join(table, "\n"))
	return strings.TrimSpace(b.String())
}

// tableReading numbers the rows and prefixes each cell with its column name.
func tableReading(rows [][]string) string {
	header, firstData := headerRow(rows)
	var b strings.Builder
	for i, row := range rows[firstData:] {
		var parts []string
		for j, cell := range row {
			if strings.TrimSpace(cell) == "" {
				continue
			}
			key := ""
			if header != nil && j < len(header) {
				key = strings.TrimSpace(header[j])
			}
			if key == "" {
				key = "第" + strconv.Itoa(j+1) + "列"
			}
			parts = append(parts, key+": "+strings.TrimSpace(cell))
		}
		if len(parts) == 0 {
			continue
		}
		fmt.Fprintf(&b, "第%d行 %s\n", i+1, strings.Join(parts, " | "))
	}
	return strings.TrimSpace(b.String())
}

// headerRow finds the caption row: the first row with more than one filled
// cell, since exports often open with a merged title row above the real header.
// It returns nil when that row looks like data rather than captions.
func headerRow(rows [][]string) ([]string, int) {
	for i, row := range rows {
		if len(nonEmptyCells(row)) < 2 {
			continue // a title or a spacer
		}
		if !looksLikeHeader(row) || i == len(rows)-1 {
			return nil, i
		}
		return row, i + 1
	}
	return nil, 0
}

// looksLikeHeader guesses whether a row names the columns below it. A numeric
// or very long cell is data, not a caption.
func looksLikeHeader(row []string) bool {
	cells := nonEmptyCells(row)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if len([]rune(c)) > 24 {
			return false
		}
		if _, err := strconv.ParseFloat(strings.ReplaceAll(c, ",", ""), 64); err == nil {
			return false
		}
	}
	return true
}

func widestRow(rows [][]string) int {
	wide := 0
	for _, row := range rows {
		if w := len(row); w > wide {
			wide = w
		}
	}
	return wide
}

func trimTrailingEmptyRows(rows [][]string) [][]string {
	end := len(rows)
	for end > 0 && len(nonEmptyCells(rows[end-1])) == 0 {
		end--
	}
	return rows[:end]
}

func nonEmptyCells(row []string) []string {
	var out []string
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
	}
	return out
}

func cellAt(row []string, i int) string {
	if i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

// readXLSX decodes the first worksheet of an .xlsx file into plain strings.
func readXLSX(raw []byte) (sheet, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return sheet{}, errors.New("不是有效的 Excel (.xlsx) 文件")
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}

	shared, err := readSharedStrings(files)
	if err != nil {
		return sheet{}, err
	}
	sheetPath, sheetName, err := findFirstSheet(files)
	if err != nil {
		return sheet{}, err
	}
	rows, err := readSheetRows(files[sheetPath], shared)
	if err != nil {
		return sheet{}, err
	}
	return sheet{Name: sheetName, Rows: rows}, nil
}

func findFirstSheet(files map[string]*zip.File) (string, string, error) {
	workbook, ok := files["xl/workbook.xml"]
	if !ok {
		return "", "", errors.New("Excel 文件缺少 workbook.xml")
	}
	raw, err := readZipFile(workbook, 8<<20)
	if err != nil {
		return "", "", err
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(raw, &wb); err != nil {
		return "", "", fmt.Errorf("解析 workbook.xml 失败: %w", err)
	}
	if len(wb.Sheets) == 0 {
		return "", "", errors.New("Excel 文件里没有工作表")
	}
	name := wb.Sheets[0].Name

	// Sheet order in workbook.xml is authoritative, so prefer it over any
	// lexicographic guess at worksheet1.xml, worksheet2.xml...
	if rels, ok := files["xl/_rels/workbook.xml.rels"]; ok {
		rawRels, err := readZipFile(rels, 4<<20)
		if err == nil {
			var relDoc struct {
				Rel []struct {
					ID     string `xml:"Id,attr"`
					Target string `xml:"Target,attr"`
				} `xml:"Relationship"`
			}
			if xml.Unmarshal(rawRels, &relDoc) == nil {
				for _, r := range relDoc.Rel {
					if r.ID == wb.Sheets[0].RID {
						target := strings.TrimPrefix(r.Target, "/")
						if !strings.HasPrefix(target, "xl/") {
							target = path.Join("xl", target)
						}
						if _, ok := files[target]; ok {
							return target, name, nil
						}
					}
				}
			}
		}
	}

	var candidates []string
	for name := range files {
		if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		return "", "", errors.New("Excel 文件里没有工作表数据")
	}
	sort.Strings(candidates)
	return candidates[0], name, nil
}

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	f, ok := files["xl/sharedStrings.xml"]
	if !ok {
		return nil, nil
	}
	raw, err := readZipFile(f, 32<<20)
	if err != nil {
		return nil, err
	}
	var doc struct {
		SI []struct {
			T string   `xml:"t"`
			R []string `xml:"r>t"`
		} `xml:"si"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("解析 sharedStrings.xml 失败: %w", err)
	}
	out := make([]string, 0, len(doc.SI))
	for _, si := range doc.SI {
		if si.T != "" {
			out = append(out, si.T)
			continue
		}
		out = append(out, strings.Join(si.R, ""))
	}
	return out, nil
}

func readSheetRows(f *zip.File, shared []string) ([][]string, error) {
	raw, err := readZipFile(f, 32<<20)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))

	var rows [][]string
	var current []string
	col := 0
	inRow := false
	inCell := false
	cellType := ""
	cellRef := ""
	var value strings.Builder

	flushCell := func() {
		text := strings.TrimSpace(value.String())
		if cellType == "s" {
			if idx, err := strconv.Atoi(text); err == nil && idx >= 0 && idx < len(shared) {
				text = shared[idx]
			}
		}
		if cellRef != "" {
			if refCol := columnIndex(cellRef); refCol > col {
				for len(current) < refCol {
					current = append(current, "")
				}
				col = refCol
			}
		}
		for len(current) < col {
			current = append(current, "")
		}
		current = append(current, text)
		col++
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析工作表失败: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				inRow, current, col = true, nil, 0
			case "c":
				if !inRow {
					continue
				}
				inCell, value, cellRef = true, strings.Builder{}, ""
				cellType = ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "t":
						cellType = a.Value
					case "r":
						cellRef = a.Value
					}
				}
			case "v", "t":
				if inCell {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						value.WriteString(s)
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "c":
				if inCell {
					flushCell()
					inCell = false
				}
			case "row":
				if inRow {
					rows = append(rows, current)
					inRow = false
				}
			}
		}
		if len(rows) >= maxSheetRows {
			break
		}
	}

	// Pad every row to the same width so the header lookup never indexes past
	// the end, and trim trailing empty rows Excel likes to keep around.
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	if width > 200 {
		width = 200
	}
	for i := range rows {
		for len(rows[i]) < width {
			rows[i] = append(rows[i], "")
		}
		if len(rows[i]) > width {
			rows[i] = rows[i][:width]
		}
	}
	for len(rows) > 0 && isEmptyRow(rows[len(rows)-1]) {
		rows = rows[:len(rows)-1]
	}
	return rows, nil
}

func isEmptyRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// columnIndex turns "C12" into 2 (zero-based).
func columnIndex(ref string) int {
	col := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		col = col*26 + int(r-'A'+1)
	}
	if col <= 0 {
		return 0
	}
	return col - 1
}

func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

// writeXLSX builds a one-sheet workbook from rows. Every cell is written as an
// inline string, which keeps the writer free of a shared-string table and is
// exactly what a filled-in form needs.
func writeXLSX(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	add := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, body)
		return err
	}

	sheetBody := &strings.Builder{}
	sheetBody.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sheetBody.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		fmt.Fprintf(sheetBody, `<row r="%d">`, r+1)
		for c, value := range row {
			if strings.TrimSpace(value) == "" {
				continue
			}
			fmt.Fprintf(sheetBody,
				`<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
				columnName(c), r+1, xmlEscape(value))
		}
		sheetBody.WriteString(`</row>`)
	}
	sheetBody.WriteString(`</sheetData></worksheet>`)

	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="填写结果" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", sheetBody.String()},
	}
	for _, p := range parts {
		if err := add(p.name, p.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// columnName turns 0 into "A", 26 into "AA".
func columnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return strings.ReplaceAll(s, "&", "&amp;")
	}
	return b.String()
}
