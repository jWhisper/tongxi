package material

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"tongxi/internal/store"
)

const MaxFileBytes = 20 << 20
const MaxCharacters = 500000

type Document struct {
	Name       string
	Format     string
	Note       string
	Segments   []store.SourceSegment
	characters int
}

func (d *Document) add(location, text string) error {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
	if text == "" {
		return nil
	}
	runes := []rune(text)
	d.characters += len(runes)
	if d.characters > MaxCharacters {
		return errors.New("解析文字超过50万字，请拆分资料后导入")
	}
	for len(runes) > 0 {
		n := min(1600, len(runes))
		d.Segments = append(d.Segments, store.SourceSegment{Number: len(d.Segments) + 1, Location: location, Content: string(runes[:n])})
		runes = runes[n:]
	}
	return nil
}
func Parse(ctx context.Context, name string, data []byte) (doc Document, err error) {
	// Third-party parsers can panic on malformed input. A failed import must not
	// crash the desktop or produce an apparently valid partial source.
	defer func() {
		if recover() != nil {
			doc = Document{}
			err = errors.New("文件损坏或格式不受支持，未导入资料")
		}
	}()
	if len(data) == 0 || len(data) > MaxFileBytes {
		return doc, errors.New("文件为空或超过20MB，请换用较小文件")
	}
	doc = Document{Name: filepath.Base(name), Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."), Segments: []store.SourceSegment{}}
	switch doc.Format {
	case "txt", "md", "markdown":
		var text string
		text, err = decodeText(data, false)
		if err == nil {
			for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
				if err = ctx.Err(); err != nil {
					break
				}
				if err = doc.add(fmt.Sprintf("第%d行", i+1), line); err != nil {
					break
				}
			}
		}
	case "csv":
		err = parseCSV(ctx, &doc, data)
	case "docx":
		err = parseDOCX(ctx, &doc, data)
	case "xlsx":
		err = parseXLSX(ctx, &doc, data)
	case "pdf":
		err = parsePDF(ctx, &doc, data)
	case "png", "jpg", "jpeg", "webp":
		var c image.Config
		var format string
		c, format, err = imageConfig(data)
		if doc.Format == "jpg" {
			doc.Format = "jpeg"
		}
		if err == nil && format != doc.Format {
			err = errors.New("图片扩展名与实际格式不一致，请使用正确的文件扩展名")
		}
		doc.Note = fmt.Sprintf("图片 · %d × %d", c.Width, c.Height)
	default:
		err = errors.New("支持文字、PDF、Word、Excel、CSV 及 PNG、JPEG、WebP 图片")
	}
	if err != nil {
		return Document{}, err
	}
	if len(doc.Segments) == 0 && !IsImage(doc.Format) {
		return Document{}, errors.New("未提取到文字；扫描件或图片请先转为可复制文字的文件")
	}
	return doc, nil
}
func decodeText(data []byte, csvFile bool) (string, error) {
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		text, err := unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
		return string(text), err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if bytes.IndexByte(data, 0) >= 0 {
		return "", errors.New("文件含二进制内容，请另存为 UTF-8 文本")
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	if csvFile {
		text, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if err == nil {
			return string(text), nil
		}
	}
	return "", errors.New("文字编码无法识别，请另存为 UTF-8 后导入")
}
func parseCSV(ctx context.Context, doc *Document, data []byte) error {
	text, err := decodeText(data, true)
	if err != nil {
		return err
	}
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	for row := 1; ; row++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		cells, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("CSV 第%d条记录无法解析：请检查引号和分隔符", row)
		}
		if err = doc.add(fmt.Sprintf("第%d条记录", row), rowText(cells)); err != nil {
			return err
		}
	}
	doc.Note = "按逗号分隔解析，记录号包含表头；跨行单元格算同一条记录。"
	return nil
}
func rowText(cells []string) string {
	values := []string{}
	for i, value := range cells {
		if strings.TrimSpace(value) != "" {
			col, _ := excelize.ColumnNumberToName(i + 1)
			values = append(values, col+"="+value)
		}
	}
	return strings.Join(values, " | ")
}
func parseXLSX(ctx context.Context, doc *Document, data []byte) error {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 100 << 20, UnzipXMLSizeLimit: 20 << 20})
	if err != nil {
		return errors.New("无法读取 XLSX，请检查文件是否损坏、加密或解压后过大")
	}
	defer f.Close()
	for _, sheet := range f.GetSheetList() {
		rows, e := f.Rows(sheet)
		if e != nil {
			return e
		}
		row := 0
		for rows.Next() {
			row++
			if err = ctx.Err(); err != nil {
				rows.Close()
				return err
			}
			cells, e := rows.Columns()
			if e != nil {
				rows.Close()
				return e
			}
			if err = doc.add(fmt.Sprintf("工作表「%s」第%d行", sheet, row), rowText(cells)); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Error()
		rows.Close()
		if err != nil {
			return err
		}
	}
	doc.Note = "读取各工作表保存的单元格值（含隐藏工作表）；公式使用文件中的缓存结果，不重算、不执行宏。合并单元格仅首格有值，图表和图片未解析。"
	return nil
}
func parseDOCX(ctx context.Context, doc *Document, data []byte) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("无法读取 DOCX 压缩结构")
	}
	var body []byte
	for _, file := range z.File {
		if file.Name != "word/document.xml" {
			continue
		}
		if file.UncompressedSize64 > 20<<20 {
			return errors.New("Word 正文解压后过大，请拆分")
		}
		r, e := file.Open()
		if e != nil {
			return e
		}
		body, e = io.ReadAll(io.LimitReader(r, (20<<20)+1))
		r.Close()
		if e != nil {
			return e
		}
		if len(body) > 20<<20 {
			return errors.New("Word 正文解压后过大")
		}
		break
	}
	if body == nil {
		return errors.New("DOCX 缺少正文文档")
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	var p strings.Builder
	paragraph, table, row, cell := 0, 0, 0, 0
	inText, inParagraph, inTable := false, false, false
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		token, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("Word 正文 XML 无法解析")
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tbl":
				table++
				row = 0
				inTable = true
			case "tr":
				row++
				cell = 0
			case "tc":
				cell++
			case "p":
				paragraph++
				inParagraph = true
				p.Reset()
			case "t":
				inText = true
			case "tab":
				if inParagraph {
					p.WriteString("\t")
				}
			case "br":
				if inParagraph {
					p.WriteString("\n")
				}
			}
		case xml.CharData:
			if inText && inParagraph {
				p.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "tbl":
				inTable = false
			case "p":
				location := fmt.Sprintf("第%d段", paragraph)
				if inTable {
					location = fmt.Sprintf("表%d · 第%d行第%d列 · %s", table, row, cell, location)
				}
				if err = doc.add(location, p.String()); err != nil {
					return err
				}
				inParagraph = false
			}
		}
	}
	doc.Note = "读取正文与表格文字；页眉页脚、批注、图片和修订标记不作为独立资料解析。"
	return nil
}
func parsePDF(ctx context.Context, doc *Document, data []byte) error {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("无法读取 PDF，请检查是否加密或损坏")
	}
	count := r.NumPage()
	if count > 300 {
		return errors.New("PDF 超过300页，请拆分后导入")
	}
	empty := 0
	for page := 1; page <= count; page++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		p := r.Page(page)
		if p.V.IsNull() {
			empty++
			continue
		}
		text, e := p.GetPlainText(nil)
		if e != nil {
			return fmt.Errorf("PDF 第%d页文字提取失败，请换用文本版文件", page)
		}
		if strings.TrimSpace(text) == "" {
			empty++
		}
		if err = doc.add(fmt.Sprintf("第%d页", page), text); err != nil {
			return err
		}
	}
	doc.Note = "提取 PDF 文字层，不识别图片；复杂表格与多栏阅读顺序请对照原文件核实。"
	if empty > 0 {
		doc.Note += fmt.Sprintf(" %d页无可提取文字，可能为空白页或扫描页。", empty)
	}
	return nil
}
