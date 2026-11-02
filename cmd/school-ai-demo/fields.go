package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// field is one fillable slot discovered in a template's HTML.
//
// Nothing here is hand-written per form: every field comes from parsing the
// template HTML, so a new template only needs its HTML dropped into the
// template store. label is the slot's aria-label, section is the nearest
// heading or row-group caption, and hint is whatever the form itself says
// around the slot (the cell to its left, and the column caption above it).
type field struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Section string   `json:"section"`
	Hint    string   `json:"hint"`
	Kind    string   `json:"kind"` // "text" or "choice"
	Options []string `json:"options,omitempty"`
}

const (
	kindText   = "text"
	kindChoice = "choice"
)

// parsedTemplate is the result of reading one template's HTML.
type parsedTemplate struct {
	Name   string
	Fields []field
	HTML   string // the same document, annotated with data-field-id attributes
}

// cell is one table cell placed on the table's column grid.
type cell struct {
	node     *html.Node
	col      int
	width    int
	rowspan  int
	previous *html.Node // immediately preceding element sibling, nil at the row start
}

// parseTemplateHTML reads a template document and returns its fillable fields
// plus a copy of the document with each slot marked by data-field-id.
func parseTemplateHTML(raw []byte) (*parsedTemplate, error) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template html: %w", err)
	}

	out := &parsedTemplate{}
	section := "基本信息"
	title := ""

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if title == "" {
					title = textOf(n)
				}
			case "h1", "h2":
				if t := textOf(n); t != "" {
					section = t
				}
			case "table":
				tableSection := section
				if label := attr(n, "aria-label"); label != "" && label != title {
					// "学生信息采集登记表填写区域" is the title with a suffix,
					// not a second heading: the section should stay the title.
					if title == "" || !strings.HasPrefix(label, title) {
						tableSection = label
					}
				}
				out.Fields = append(out.Fields, annotateTable(n, tableSection, len(out.Fields))...)
				// A form also puts blanks outside the grid: the date and serial
				// line in the collection sheet, for example.
				annotateStranded(n, tableSection, &out.Fields)
				return // annotateTable owns this subtree
			}
			// A slot on a metadata line ("登记日期：____") is not in any table.
			if isTextSlot(n) && attr(n, slotMarker) == "" {
				if f, ok := strandedField(n, section); ok {
					markSlot(n, len(out.Fields))
					out.Fields = append(out.Fields, f)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	out.Name = title
	if out.Name == "" {
		out.Name = "未命名模板"
	}

	for i := range out.Fields {
		out.Fields[i].ID = fmt.Sprintf("f%02d", i+1)
	}
	applyFieldIDs(doc, out.Fields)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return nil, fmt.Errorf("render template html: %w", err)
	}
	out.HTML = buf.String()
	return out, nil
}

// slotMarker is the attribute parseTemplateHTML writes to bind a field to its
// place in the document. The value is a document-order index while parsing.
const slotMarker = "data-slot-index"

// rowGroup is one scope="rowgroup" block: a sub-table inside the table with
// its own caption. Two groups can sit side by side in one row (兴趣爱好 and
// 个人特长), so a cell belongs to the group whose column starts nearest to
// its left, not simply to whichever caption was seen last.
type rowGroup struct {
	text  string
	col   int
	from  int // first row index the group covers
	until int // last row index the group covers
}

// annotateTable finds the fillable slots inside one table.
func annotateTable(table *html.Node, section string, start int) []field {
	rows := tableRows(table)

	// Pass one: lay the whole table out once and record every row-group
	// caption, so the second pass knows all of them, including captions that
	// come later in the same row.
	groups := collectRowGroups(rows)

	headers := map[int]string{}
	if thead := findFirst(table, "thead"); thead != nil {
		if tr := findFirst(thead, "tr"); tr != nil {
			for _, c := range placeCells(tr, map[int]int{}) {
				headers[c.col] = textOf(c.node)
			}
		}
	}

	var fields []field
	occupied := map[int]int{}
	wasGrouped := false

	for rowIdx, tr := range rows {
		cells := placeCells(tr, occupied)

		// Leaving a row-group block also ends its column captions. Without
		// this, 现就读班级 would inherit 起止年月 from the 曾就读学校 block
		// above it and read as a date field.
		stillGrouped := false
		for _, g := range groups {
			if rowIdx >= g.from && rowIdx <= g.until {
				stillGrouped = true
				break
			}
		}
		if wasGrouped && !stillGrouped {
			headers = map[int]string{}
		}
		wasGrouped = stillGrouped

		rowIsHeader := false
		for _, c := range cells {
			if c.node.Data != "th" || attr(c.node, "scope") != "col" {
				continue
			}
			rowIsHeader = true
			for i := 0; i < c.width; i++ {
				headers[c.col+i] = textOf(c.node)
			}
		}
		if rowIsHeader {
			continue
		}

		for _, c := range cells {
			node := c.node
			rowSection := section
			if g, ok := groupAt(groups, rowIdx, c.col); ok {
				rowSection = g.text
			}

			switch {
			case isTextSlot(node):
				f := field{
					Label:   attr(node, "aria-label"),
					Section: rowSection,
					Kind:    kindText,
					Hint:    hintFor(c, headers, ""),
				}
				if f.Label == "" {
					f.Label = hintFor(c, headers, "字段")
				}
				if f.Label == "" {
					continue
				}
				markSlot(node, start+len(fields))
				fields = append(fields, f)
			case hasClass(node, "choices") && hasRadio(node):
				options, name := radioOptions(node)
				if len(options) == 0 {
					continue
				}
				label := textOf(c.previous)
				if label == "" {
					label = name
				}
				f := field{
					Label:   label,
					Section: rowSection,
					Kind:    kindChoice,
					Options: options,
					Hint:    hintFor(c, headers, label),
				}
				markSlot(node, start+len(fields))
				fields = append(fields, f)
			}
		}
	}
	return fields
}

// collectRowGroups reads every row-group caption in the table.
func collectRowGroups(rows []*html.Node) []rowGroup {
	var groups []rowGroup
	occupied := map[int]int{}
	for rowIdx, tr := range rows {
		for _, c := range placeCells(tr, occupied) {
			if c.node.Data != "th" || attr(c.node, "scope") != "rowgroup" {
				continue
			}
			span := rowspan(c.node)
			if span <= 1 {
				continue
			}
			groups = append(groups, rowGroup{
				text:  textOf(c.node),
				col:   c.col,
				from:  rowIdx,
				until: rowIdx + span - 1,
			})
		}
	}
	return groups
}

// groupAt returns the group covering one cell: among the groups active on that
// row, the one whose caption starts nearest to the cell's left.
func groupAt(groups []rowGroup, row, col int) (rowGroup, bool) {
	var best rowGroup
	found := false
	for _, g := range groups {
		if row < g.from || row > g.until || g.col > col {
			continue
		}
		if !found || g.col > best.col {
			best, found = g, true
		}
	}
	return best, found
}

// annotateStranded picks up fillable slots that no table cell claimed, such as
// a labelled span inside a caption cell ("班主任：____"). They still matter:
// the model can fill them, so dropping them would quietly lose information the
// form asks for.
func annotateStranded(root *html.Node, section string, fields *[]field) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if attr(n, slotMarker) != "" {
				return // already a field; do not descend into its content
			}
			if isTextSlot(n) {
				if f, ok := strandedField(n, section); ok {
					markSlot(n, len(*fields))
					*fields = append(*fields, f)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// strandedField describes one slot from the text its parent puts around it,
// because a metadata line has no table cell to read a caption from.
func strandedField(n *html.Node, section string) (field, bool) {
	f := field{
		Label:   attr(n, "aria-label"),
		Section: section,
		Kind:    kindText,
		Hint:    surroundingText(n),
	}
	if f.Label == "" {
		f.Label = f.Hint
	}
	if f.Label == "" {
		return field{}, false
	}
	return f, true
}

// surroundingText is the parent's text with every fillable slot removed, so
// "登记日期：[slot] 编号：[slot]" yields "登记日期 编号" for either slot.
func surroundingText(n *html.Node) string {
	p := n.Parent
	if p == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				b.WriteString(c.Data)
			case c.Type != html.ElementNode:
				// comments and doctypes carry no caption
			case c.Data == "br":
				// a line break inside one caption, not a separator
			case isTextSlot(c) || (c.Data == "input" && attr(c, "type") == "radio"):
				// a blank in the caption, not caption text
			default:
				walk(c)
			}
		}
	}
	walk(p)
	return strings.TrimRight(strings.Join(strings.Fields(b.String()), " "), " ：:")
}

// hintFor builds the disambiguating text shown to the model next to a field:
// the caption cell on the slot's left plus the column caption above it.
func hintFor(c cell, headers map[int]string, self string) string {
	var parts []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || s == self || seen[s] {
			return
		}
		seen[s] = true
		parts = append(parts, s)
	}
	if c.previous != nil && !isSlotNode(c.previous) && (c.previous.Data == "th" || c.previous.Data == "td") {
		add(textOf(c.previous))
	}
	add(headers[c.col])
	return strings.TrimRight(strings.Join(parts, " / "), " ：:")
}

// placeCells lays a row out on the table grid and remembers how many rows the
// grid track spans, so rowspan cells from earlier rows stay counted.
func placeCells(tr *html.Node, occupied map[int]int) []cell {
	for k, v := range occupied {
		if v > 0 {
			occupied[k] = v - 1
		}
	}
	var cells []cell
	col := 0
	var previous *html.Node
	for n := tr.FirstChild; n != nil; n = n.NextSibling {
		if n.Type != html.ElementNode || (n.Data != "td" && n.Data != "th") {
			continue
		}
		for occupied[col] > 0 {
			col++
		}
		width := colspan(n)
		span := rowspan(n)
		if span > 1 {
			for i := 0; i < width; i++ {
				occupied[col+i] = span
			}
		}
		cells = append(cells, cell{node: n, col: col, width: width, rowspan: span, previous: previous})
		col += width
		previous = n
	}
	return cells
}

// tableRows lists the table's rows in document order, skipping nested tables.
func tableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				walk(c)
				continue
			}
			switch c.Data {
			case "tr":
				rows = append(rows, c)
			case "table":
				// nested tables are not expected in these forms
			default:
				walk(c)
			}
		}
	}
	walk(table)
	return rows
}

// applyFieldIDs replaces the parse-time markers with the stable ids.
func applyFieldIDs(doc *html.Node, fields []field) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for i, a := range n.Attr {
			if a.Key != slotMarker {
				continue
			}
			idx, err := strconv.Atoi(a.Val)
			if err != nil || idx < 0 || idx >= len(fields) {
				continue
			}
			n.Attr[i].Key = "data-field-id"
			n.Attr[i].Val = fields[idx].ID
			if fields[idx].Kind == kindChoice {
				n.Attr = append(n.Attr, html.Attribute{Key: "data-field-kind", Val: kindChoice})
			}
			// The photo cell ships with the word 照片 in it. That is a
			// placeholder, not an answer: without recording it, an untouched
			// slot reads back as filled and ends up in the exported sheet.
			if ph := textOf(n); ph != "" && fields[idx].Kind == kindText {
				n.Attr = append(n.Attr, html.Attribute{Key: "data-placeholder", Val: ph})
			}
			break
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

func markSlot(n *html.Node, index int) {
	n.Attr = append(n.Attr, html.Attribute{Key: slotMarker, Val: strconv.Itoa(index)})
}

func findFirst(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, name string) bool {
	if n == nil {
		return false
	}
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == name {
			return true
		}
	}
	return false
}

func intAttr(n *html.Node, key string, fallback int) int {
	if v := attr(n, key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func colspan(n *html.Node) int { return intAttr(n, "colspan", 1) }
func rowspan(n *html.Node) int { return intAttr(n, "rowspan", 1) }

func isTextSlot(n *html.Node) bool {
	if n == nil || (n.Data != "td" && n.Data != "th" && n.Data != "span") {
		return false
	}
	return strings.EqualFold(attr(n, "contenteditable"), "true")
}

func isSlotNode(n *html.Node) bool {
	return isTextSlot(n) || hasRadio(n)
}

func hasRadio(n *html.Node) bool {
	return n != nil && findRadio(n) != nil
}

func findRadio(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "input" && attr(n, "type") == "radio" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findRadio(c); found != nil {
			return found
		}
	}
	return nil
}

// radioOptions returns the choice labels of a radio group and its input name.
func radioOptions(n *html.Node) ([]string, string) {
	var options []string
	name := ""
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "label" {
			if label := strings.TrimSpace(textOf(node)); label != "" {
				options = append(options, label)
			}
			return
		}
		if node.Type == html.ElementNode && node.Data == "input" && attr(node, "type") == "radio" {
			if name == "" {
				name = attr(node, "name")
			}
			if v := attr(node, "value"); v != "" {
				options = append(options, strings.TrimSpace(v))
			}
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return dedupe(options), name
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := values[:0]
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// textOf flattens an element's text, treating <br> and block boundaries as
// spaces, so "家庭住址<br>（详细）" reads as one phrase.
func textOf(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		switch node.Type {
		case html.TextNode:
			b.WriteString(node.Data)
		case html.ElementNode:
			// <br> inside a Chinese caption is a line break, not a word gap:
			// "家庭住址<br>（详细）" has to read as 家庭住址（详细）.
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
