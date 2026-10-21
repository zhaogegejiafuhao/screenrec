package main

// 纪要导出：把转写结果整理成按说话人分段的 Markdown / Word。
// 不依赖任何第三方库——docx 就是一个固定结构的 zip，用标准库手写即可。

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"

	"screenrec/internal/mathnorm"
)

// exportBlock 一段连续、同一说话人的内容
type exportBlock struct {
	Speaker int
	Start   float64
	Text    string
	Trans   string
}

// buildBlocks 把逐句结果合并成「按说话人 + 段落」的结构
func buildBlocks(segs []Segment, doMath bool) []exportBlock {
	var out []exportBlock
	for _, s := range segs {
		text := s.Text
		if doMath {
			text = mathnorm.Normalize(text)
		}
		if text == "" {
			continue
		}
		n := len(out)
		if n > 0 && out[n-1].Speaker == s.Speaker {
			out[n-1].Text = joinSentence(out[n-1].Text, text)
			out[n-1].Trans = joinSentence(out[n-1].Trans, s.Trans)
			continue
		}
		out = append(out, exportBlock{Speaker: s.Speaker, Start: s.Start, Text: text, Trans: s.Trans})
	}
	return out
}

// joinSentence 拼接句子：中文直接连，英文之间补空格
func joinSentence(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	last := a[len(a)-1]
	first := b[0]
	if last < 128 && first < 128 && last != ' ' {
		// 英文句末标点后要补空格；中文标点是多字节，不会走到这里
		if last == '.' || last == '!' || last == '?' || last == ',' || last == ';' || last == ':' {
			return a + " " + b
		}
		if isWordByte(last) && isWordByte(first) {
			return a + " " + b
		}
	}
	return a + b
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// clock 把秒格式化成 00:12 / 1:02:03
func clock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	t := int(sec + 0.5)
	h, m, s := t/3600, (t%3600)/60, t%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// durationText 人类可读的时长
func durationText(ms int64) string {
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%d 秒", sec)
	}
	return fmt.Sprintf("%d 分 %d 秒", sec/60, sec%60)
}

// speakerName 说话人标签。没做分离时返回空串，避免每段都写「说话人 0」
func speakerName(rec *Recording, n int) string {
	if n <= 0 || rec.Speakers <= 0 {
		return ""
	}
	return fmt.Sprintf("说话人 %d", n)
}

func metaLines(rec *Recording) []string {
	lines := []string{
		"时间：" + rec.StartedAt.Format("2006-01-02 15:04"),
		"时长：" + durationText(rec.DurationMs),
	}
	if rec.Speakers > 0 {
		lines = append(lines, fmt.Sprintf("说话人：%d 人（已区分）", rec.Speakers))
	}
	src := rec.Mic
	if rec.SystemAudio {
		if src != "" {
			src += "；"
		}
		src += "系统声音"
		if rec.SysDevice == deviceProcessLabel && rec.SysDevice != "" {
			src += "（全部应用）"
		}
	}
	if src != "" {
		lines = append(lines, "来源："+src)
	}
	if rec.SysWarning != "" {
		lines = append(lines, "提示："+rec.SysWarning)
	}
	if rec.TranslateDir != "" {
		lines = append(lines, "字幕：双语（"+directionName(rec.TranslateDir)+"）")
	}
	return lines
}

// deviceProcessLabel 进程级回环的伪设备名（避免直接显示 __process__）
const deviceProcessLabel = "__process__"

// buildMarkdown 生成纪要 Markdown
func buildMarkdown(rec *Recording, doMath bool) string {
	blocks := buildBlocks(rec.Segments, doMath)
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", rec.Title)
	for _, l := range metaLines(rec) {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	b.WriteString("\n---\n\n")

	if len(blocks) == 0 {
		b.WriteString("_（没有识别到语音内容）_\n")
		return b.String()
	}
	for _, blk := range blocks {
		name := speakerName(rec, blk.Speaker)
		if name != "" {
			fmt.Fprintf(&b, "**%s** `%s`\n\n", name, clock(blk.Start))
		} else {
			fmt.Fprintf(&b, "`%s`\n\n", clock(blk.Start))
		}
		b.WriteString(blk.Text)
		b.WriteString("\n\n")
		if blk.Trans != "" {
			b.WriteString("> " + blk.Trans + "\n\n")
		}
	}
	return b.String()
}

// ---- 最小可用的 docx 生成 ----

type docRun struct {
	Text   string
	Bold   bool
	Italic bool
	Color  string // 六位十六进制，不带 #
	Size   int    // 半磅
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func runXML(r docRun) string {
	var b strings.Builder
	b.WriteString("<w:r>")
	if r.Bold || r.Italic || r.Color != "" || r.Size > 0 {
		b.WriteString("<w:rPr>")
		if r.Bold {
			b.WriteString("<w:b/><w:bCs/>")
		}
		if r.Italic {
			b.WriteString("<w:i/><w:iCs/>")
		}
		if r.Color != "" {
			b.WriteString(`<w:color w:val="` + r.Color + `"/>`)
		}
		if r.Size > 0 {
			fmt.Fprintf(&b, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, r.Size, r.Size)
		}
		b.WriteString("</w:rPr>")
	}
	b.WriteString(`<w:t xml:space="preserve">` + xmlEscape(r.Text) + `</w:t></w:r>`)
	return b.String()
}

func paraXML(style string, line int, runs []docRun) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	if style != "" || line > 0 {
		b.WriteString("<w:pPr>")
		if style != "" {
			b.WriteString(`<w:pStyle w:val="` + style + `"/>`)
		}
		if line > 0 {
			fmt.Fprintf(&b, `<w:spacing w:line="%d" w:lineRule="auto" w:after="120"/>`, line)
		}
		b.WriteString("</w:pPr>")
	}
	for _, r := range runs {
		b.WriteString(runXML(r))
	}
	b.WriteString("</w:p>")
	return b.String()
}

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`

const docxRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults>
<w:rPrDefault><w:rPr><w:rFonts w:ascii="Segoe UI" w:hAnsi="Segoe UI" w:eastAsia="微软雅黑"/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:rPrDefault>
<w:pPrDefault><w:pPr><w:spacing w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault>
</w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:before="120" w:after="180"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/><w:szCs w:val="36"/></w:rPr></w:style>
</w:styles>`

// buildDocx 生成 Word 文档（不依赖 Word，任何支持 OOXML 的软件都能打开）
func buildDocx(rec *Recording, doMath bool) ([]byte, error) {
	blocks := buildBlocks(rec.Segments, doMath)

	var body strings.Builder
	body.WriteString(paraXML("Heading1", 0, []docRun{{Text: rec.Title}}))

	for _, l := range metaLines(rec) {
		body.WriteString(paraXML("", 280, []docRun{{Text: l, Color: "666666", Size: 19}}))
	}

	if len(blocks) == 0 {
		body.WriteString(paraXML("", 360, []docRun{{Text: "（没有识别到语音内容）", Color: "888888"}}))
	}
	for i, blk := range blocks {
		name := speakerName(rec, blk.Speaker)
		var head []docRun
		if name != "" {
			head = append(head,
				docRun{Text: name, Bold: true, Color: "1A6DFF", Size: 22},
				docRun{Text: "  " + clock(blk.Start), Color: "999999", Size: 18},
			)
		} else {
			head = append(head, docRun{Text: clock(blk.Start), Color: "999999", Size: 18})
		}
		body.WriteString(paraXML("", 300, head))
		body.WriteString(paraXML("", 360, []docRun{{Text: blk.Text}}))
		if blk.Trans != "" {
			body.WriteString(paraXML("", 300, []docRun{{Text: blk.Trans, Italic: true, Color: "777777", Size: 20}}))
		}
		_ = i
	}

	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="851" w:footer="992" w:gutter="0"/></w:sectPr></w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := []struct{ name, data string }{
		{"[Content_Types].xml", docxContentTypes},
		{"_rels/.rels", docxRels},
		{"word/document.xml", doc},
		{"word/styles.xml", docxStyles},
	}
	now := time.Now()
	for _, f := range files {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write([]byte(f.data)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
