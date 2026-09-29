package material

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// DOCX is generated as a small OOXML package: rendered text, headings, lists,
// tables, code and links only. Model HTML and remote images are never executed.
func DOCX(markdown string) ([]byte, error) {
	source := []byte(markdown)
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	d := wordDocument{source: source}
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		d.block(n, 0)
	}
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` + d.body.String() + `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"/></w:sectPr></w:body></w:document>`
	relationships := `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="styles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` + strings.Join(d.rels, "") + `</Relationships>`
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="document" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", document}, {"word/_rels/document.xml.rels", relationships},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="PingFang SC"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="140" w:line="300" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:after="220"/></w:pPr><w:rPr><w:b/><w:color w:val="000000"/><w:sz w:val="40"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="300" w:after="180"/></w:pPr><w:rPr><w:b/><w:color w:val="193249"/><w:sz w:val="36"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Heading1"/><w:rPr><w:sz w:val="28"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Heading2"/><w:rPr><w:sz w:val="24"/></w:rPr></w:style></w:styles>`},
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, part := range parts {
		w, err := z.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(part.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type wordDocument struct {
	source      []byte
	body        strings.Builder
	rels        []string
	bookmarks   int
	sourceEntry bool
}

func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func (d *wordDocument) run(value, properties string) {
	d.body.WriteString("<w:r><w:rPr>" + properties + "</w:rPr>")
	for i, line := range strings.Split(value, "\n") {
		if i > 0 {
			d.body.WriteString("<w:br/>")
		}
		d.body.WriteString(`<w:t xml:space="preserve">` + escape(line) + `</w:t>`)
	}
	d.body.WriteString("</w:r>")
}
func (d *wordDocument) inline(n ast.Node, properties string) {
	switch v := n.(type) {
	case *ast.Text:
		value := v.Segment.Value(d.source)
		if n.Parent() == nil || n.Parent().Kind() != ast.KindCodeSpan {
			value = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(value)))
		}
		d.run(string(value), properties)
		if v.HardLineBreak() {
			d.run("\n", properties)
		} else if v.SoftLineBreak() {
			d.run(" ", properties)
		}
		return
	case *ast.String:
		d.run(string(v.Value), properties)
		return
	case *ast.Emphasis:
		if v.Level == 2 {
			properties += "<w:b/>"
		} else {
			properties += "<w:i/>"
		}
	case *ast.CodeSpan:
		properties += `<w:rFonts w:ascii="Menlo" w:hAnsi="Menlo"/><w:shd w:fill="EEF3F6"/>`
	case *extast.Strikethrough:
		properties += "<w:strike/>"
	case *ast.Link:
		destination := string(v.Destination)
		if strings.HasPrefix(destination, "#source-") {
			d.body.WriteString(`<w:hyperlink w:anchor="` + escape(strings.TrimPrefix(destination, "#")) + `">`)
		} else if strings.HasPrefix(destination, "https://") || strings.HasPrefix(destination, "http://") {
			id := fmt.Sprintf("link%d", len(d.rels)+1)
			d.rels = append(d.rels, `<Relationship Id="`+id+`" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="`+escape(destination)+`" TargetMode="External"/>`)
			d.body.WriteString(`<w:hyperlink r:id="` + id + `">`)
		} else {
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				d.inline(c, properties)
			}
			return
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			d.inline(c, properties+`<w:color w:val="206B61"/><w:u w:val="single"/>`)
		}
		d.body.WriteString("</w:hyperlink>")
		return
	case *ast.AutoLink:
		d.run(string(v.URL(d.source)), properties)
		return
	case *ast.Image:
		d.run("[图片："+string(v.Text(d.source))+"]", properties)
		return
	case *ast.RawHTML:
		return
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		d.inline(c, properties)
	}
}
func (d *wordDocument) paragraph(n ast.Node, style, prefix string, depth int) {
	d.body.WriteString("<w:p><w:pPr>")
	if d.sourceEntry {
		d.body.WriteString(`<w:keepLines/><w:spacing w:before="0" w:after="80" w:line="240" w:lineRule="auto"/>`)
		if depth == 0 {
			d.body.WriteString("<w:keepNext/>")
		}
	}
	if style != "" {
		d.body.WriteString(`<w:pStyle w:val="` + style + `"/>`)
	}
	if depth > 0 {
		d.body.WriteString(fmt.Sprintf(`<w:ind w:left="%d"/>`, depth*360))
	}
	d.body.WriteString("</w:pPr>")
	title := string(n.Text(d.source))
	bookmark := ""
	if strings.HasPrefix(title, "来源 ") {
		if number, err := strconv.Atoi(strings.TrimPrefix(title, "来源 ")); err == nil {
			bookmark = fmt.Sprintf("source-%d", number)
			d.bookmarks++
			d.body.WriteString(fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>`, d.bookmarks, bookmark))
		}
	}
	if prefix != "" {
		d.run(prefix, "")
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		d.inline(c, "")
	}
	if bookmark != "" {
		d.body.WriteString(fmt.Sprintf(`<w:bookmarkEnd w:id="%d"/>`, d.bookmarks))
	}
	d.body.WriteString("</w:p>")
}
func (d *wordDocument) block(n ast.Node, depth int) {
	switch v := n.(type) {
	case *ast.Heading:
		d.sourceEntry = strings.HasPrefix(string(n.Text(d.source)), "来源 ")
		style := fmt.Sprintf("Heading%d", min(v.Level, 3))
		if n.PreviousSibling() == nil && n.Parent() != nil && n.Parent().Kind() == ast.KindDocument && v.Level == 1 {
			style = "Title"
		}
		d.paragraph(n, style, "", depth)
	case *ast.Paragraph, *ast.TextBlock:
		d.paragraph(n, "", "", depth)
	case *ast.List:
		index := v.Start
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			prefix := "• "
			if v.IsOrdered() {
				prefix = fmt.Sprintf("%d. ", index)
				index++
			}
			first := true
			for c := item.FirstChild(); c != nil; c = c.NextSibling() {
				if first && (c.Kind() == ast.KindParagraph || c.Kind() == ast.KindTextBlock) {
					d.paragraph(c, "", prefix, depth+1)
				} else {
					d.block(c, depth+1)
				}
				first = false
			}
		}
	case *ast.Blockquote:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			d.block(c, depth+1)
		}
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		d.body.WriteString("<w:p><w:pPr><w:shd w:fill=\"EEF3F6\"/></w:pPr>")
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			d.run(string(line.Value(d.source)), `<w:rFonts w:ascii="Menlo" w:hAnsi="Menlo"/><w:sz w:val="19"/>`)
		}
		d.body.WriteString("</w:p>")
	case *extast.Table:
		d.body.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders><w:top w:val="single" w:sz="4" w:color="DCE5EB"/><w:left w:val="single" w:sz="4" w:color="DCE5EB"/><w:bottom w:val="single" w:sz="4" w:color="DCE5EB"/><w:right w:val="single" w:sz="4" w:color="DCE5EB"/><w:insideH w:val="single" w:sz="4" w:color="DCE5EB"/><w:insideV w:val="single" w:sz="4" w:color="DCE5EB"/></w:tblBorders></w:tblPr><w:tblGrid>`)
		for range v.Alignments {
			d.body.WriteString(`<w:gridCol w:w="2400"/>`)
		}
		d.body.WriteString("</w:tblGrid>")
		for row := n.FirstChild(); row != nil; row = row.NextSibling() {
			d.body.WriteString("<w:tr>")
			if row.Kind() == extast.KindTableHeader {
				d.body.WriteString("<w:trPr><w:tblHeader/></w:trPr>")
			}
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				d.body.WriteString("<w:tc><w:tcPr><w:tcW w:w=\"2400\" w:type=\"dxa\"/></w:tcPr>")
				d.paragraph(cell, "", "", 0)
				d.body.WriteString("</w:tc>")
			}
			d.body.WriteString("</w:tr>")
		}
		d.body.WriteString("</w:tbl>")
	case *ast.ThematicBreak:
		d.body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="4" w:color="DCE5EB"/></w:pBdr></w:pPr></w:p>`)
	case *ast.HTMLBlock:
		return
	default:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			d.block(c, depth)
		}
	}
}
