package material

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func pdfFixture() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	stream := "BT /F1 12 Tf 50 700 Td (Budget 100 yuan) Tj ET"
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>`, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)}
	offsets := []int{0}
	for i, s := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, s)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(objects)+1, xref)
	return b.Bytes()
}
func TestImportSixFormatsAndStableLocations(t *testing.T) {
	ctx := context.Background()
	docx, err := DOCX("# 活动资料\n\n人数12人，预算100元。\n\n| 项目 | 金额 |\n| --- | --- |\n| 茶点 | 80元 |")
	if err != nil {
		t.Fatal(err)
	}
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "预算")
	f.SetCellValue("预算", "A1", "项目")
	f.SetCellValue("预算", "B1", "金额")
	f.SetCellValue("预算", "A3", "茶点")
	f.SetCellValue("预算", "B3", 80)
	f.SetCellFormula("预算", "B3", "40*2")
	buffer, err := f.WriteToBuffer()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name           string
		data           []byte
		text, location string
	}{
		{"资料.txt", []byte("人数12人\n预算100元"), "预算100元", "第2行"},
		{"资料.md", []byte("# 活动\n时长30分钟"), "时长30分钟", "第2行"},
		{"资料.csv", []byte("项目,备注\n茶点,\"预算100元\n可调整\""), "B=预算100元\n可调整", "第2条记录"},
		{"资料.docx", docx, "80元", "表1 · 第2行第2列"},
		{"资料.xlsx", buffer.Bytes(), "B=80", "工作表「预算」第3行"},
		{"资料.pdf", pdfFixture(), "Budget 100 yuan", "第1页"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, e := Parse(ctx, c.name, c.data)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, part := range d.Segments {
				if strings.Contains(part.Content, c.text) && strings.Contains(part.Location, c.location) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing located content %#v", d.Segments)
			}
		})
	}
}
func TestParserRejectsUnusableFilesAndPreservesUnicodeChunks(t *testing.T) {
	for name, data := range map[string][]byte{"bad.docx": []byte("broken"), "bad.pdf": []byte("broken"), "bad.xlsx": []byte("broken"), "old.xls": []byte("binary"), "empty.txt": {}, "bad.csv": []byte("a,\"broken"), "binary.txt": {0, 1, 2}, "large.txt": bytes.Repeat([]byte("a"), MaxFileBytes+1), "long.txt": []byte(strings.Repeat("字", MaxCharacters+1))} {
		if d, err := Parse(context.Background(), name, data); err == nil || len(d.Segments) != 0 {
			t.Fatalf("accepted invalid %s: %v", name, err)
		}
	}
	text := strings.Repeat("你好🙂", 1200)
	d, err := Parse(context.Background(), "unicode.md", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	combined := ""
	for _, p := range d.Segments {
		combined += p.Content
		if p.Location != "第1行" || len([]rune(p.Content)) > 1600 {
			t.Fatal(p)
		}
	}
	if combined != text {
		t.Fatal("lost Unicode text")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Parse(ctx, "cancel.txt", []byte("内容")); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestDOCXPackageKeepsTablesUnicodeLinksAndEscapesXML(t *testing.T) {
	data, err := DOCX("# 标题 & 成果\n\n**加粗** *斜体* [原网页](https://example.com/?a=1&b=2) [资料](#source-1)\n\n- 第一项\n- 第二项\n\n| 姓名 | 数字 |\n| --- | --- |\n| 你好🙂 | 12 |\n\n### 来源 1\n\n> 原文 & 摘录\n\n<script>danger()</script>")
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		parts[f.Name] = string(b)
	}
	xml := parts["word/document.xml"]
	for _, want := range []string{"<w:tbl>", "你好🙂", "&amp;", "<w:b/>", "<w:i/>", `w:anchor="source-1"`, `w:name="source-1"`} {
		if !strings.Contains(xml, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(xml, "danger()") {
		t.Fatal("raw HTML executed as content")
	}
	if !strings.Contains(parts["word/_rels/document.xml.rels"], "a=1&amp;b=2") {
		t.Fatal("external link broken")
	}
}

func TestDOCXEscapedProseAndLiteralCode(t *testing.T) {
	data, err := DOCX("A\\_B\\[计划\\] &lt;x&gt; &amp; &#20013;\n\n`A\\_B &lt;x&gt;`")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(context.Background(), "escaped.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Segments[0].Content; got != "A_B[计划] <x> & 中" {
		t.Fatalf("prose = %q", got)
	}
	if got := doc.Segments[1].Content; got != `A\_B &lt;x&gt;` {
		t.Fatalf("code = %q", got)
	}
}
