package mtwire

// 财务报表导出层（S3）：把 S2 渲染好的 reportExportTable 编码为 CSV / PDF 并流式写出。
//
// 契约（见 doc/finance-report-contract.md §1.4/§1.7 + report.go 的 reportExportTable）：
//   - 入参 reportExportTable 已由 detail handler 完成全部格式化（金额 2 位小数、时间 ISO-8601、
//     列序对齐 JSON 字段名、代理 scope 已省略 tenant_id/agent_name 列）。本层只负责编码 + 写出，
//     不做任何再格式化。
//   - Filename 不含扩展名；本层按格式追加 ".csv"/".pdf"。
//   - 关键流式纪律：先在内存 bytes.Buffer 完整渲染，**成功**才向 c.Writer 写首字节；渲染失败
//     返回非 nil error（不写任何字节），由调用方（exportFinanceDetail）回落干净的
//     REPORT_EXPORT_FAILED JSON。errReportExportFailed 已在 report.go 声明，本层不重复声明。
//   - reportExportTable 类型亦在 report.go（同包）声明，本层不重复声明。
//
// CSV：标准库 encoding/csv；首部加 UTF-8 BOM 以便 Excel 正确渲染中文/¥；表头 = JSON 字段名。
// PDF：**零依赖**——仅用标准库手写最小可用 PDF（%PDF-1.4 + Catalog/Pages/Page/Font(Helvetica,
//      标准 14 字体不内嵌) + 内容流 BT/Td/Tj 文本 + 'm/l/S' 画线 + 精确 xref/trailer + %%EOF）。
//      支持多页（按行分页）。标准 14 Helvetica 用 WinAnsiEncoding，**无法呈现中文等非拉丁字形**：
//      非 ASCII 字符 ASCII-fold 为 '?'（表头为英文、值为数字/ID，故业务可读性不受影响；CSV 为无损导出）。

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// writeDetailCSV 把已渲染表格编码为带 UTF-8 BOM 的 CSV 并以 attachment 流式写出。
// 成功返回 nil；编码失败返回非 nil（此前未向 c.Writer 写任何字节）。
func writeDetailCSV(c *gin.Context, table reportExportTable) error {
	var buf bytes.Buffer
	// UTF-8 BOM：让 Excel 以 UTF-8 解码，正确渲染中文与 ¥。
	buf.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(&buf)
	w.UseCRLF = true // Windows/Excel 友好的行结束符
	if err := w.Write(table.Headers); err != nil {
		return err
	}
	for _, row := range table.Rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	// 渲染成功 → 设置头并写出（c.Data 在首字节时写 200 + Content-Type）。
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, table.Filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	return nil
}

// writeDetailPDF 把已渲染表格编码为零依赖最小 PDF（多页）并以 attachment 流式写出。
// 成功返回 nil；渲染失败返回非 nil（此前未向 c.Writer 写任何字节）。
func writeDetailPDF(c *gin.Context, table reportExportTable) error {
	doc, err := renderTablePDF(table)
	if err != nil {
		return err
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, table.Filename))
	c.Data(http.StatusOK, "application/pdf", doc)
	return nil
}

// ============================================================================
// 零依赖 PDF 写出器（仅标准库）
// ============================================================================

// PDF 页面与排版常量（单位 = PDF point，1/72 inch）。US Letter 竖版。
const (
	pdfPageW       = 612.0
	pdfPageH       = 792.0
	pdfMarginX     = 50.0
	pdfMarginTop   = 50.0
	pdfMarginBot   = 50.0
	pdfTitleSize   = 14
	pdfHeaderSize  = 9
	pdfDataSize    = 8
	pdfRowH        = 14.0
	pdfApproxCharW = 5.0 // 估算字符宽度（用于按列宽截断，偏保守以避免列间重叠）
)

// renderTablePDF 把表格渲染为完整 PDF 字节流（含精确 xref/trailer）。
// 任何阶段失败返回非 nil；成功返回完整文档（调用方在成功后才写 c.Writer）。
func renderTablePDF(table reportExportTable) ([]byte, error) {
	numCols := len(table.Headers)
	if numCols == 0 {
		numCols = 1
	}

	// 列布局：在可用宽度内等分；按列宽估算可容字符数用于截断。
	usableW := pdfPageW - 2*pdfMarginX
	colW := usableW / float64(numCols)
	colX := make([]float64, numCols)
	maxChars := make([]int, numCols)
	for i := 0; i < numCols; i++ {
		colX[i] = pdfMarginX + float64(i)*colW
		mc := int(colW / pdfApproxCharW)
		if mc < 4 {
			mc = 4
		}
		maxChars[i] = mc
	}

	// 行排版几何：标题 → 表头 → 数据行；据此推导每页行数。
	titleY := pdfPageH - pdfMarginTop
	headerY := titleY - 28.0
	firstRowY := headerY - 16.0
	rowsPerPage := int((firstRowY - pdfMarginBot) / pdfRowH)
	if rowsPerPage < 1 {
		rowsPerPage = 1
	}

	// 分页：把数据行切成每页 rowsPerPage 行；零行也产出一页（标题 + 表头 + 占位）。
	pages := paginateRows(table.Rows, rowsPerPage)
	numPages := len(pages)

	// 对象编号：1=Catalog 2=Pages 3=Font；其后每页占 2 个对象（Page、Content）。
	numObjects := 3 + 2*numPages

	// 构建各对象体（索引 k 对应对象号 k+1）。
	objs := make([][]byte, 0, numObjects)

	// obj1 Catalog
	objs = append(objs, []byte("<< /Type /Catalog /Pages 2 0 R >>"))

	// obj2 Pages（MediaBox/Resources 由各 Page 继承）
	var kids strings.Builder
	for i := 0; i < numPages; i++ {
		if i > 0 {
			kids.WriteByte(' ')
		}
		fmt.Fprintf(&kids, "%d 0 R", 4+2*i)
	}
	objs = append(objs, []byte(fmt.Sprintf(
		"<< /Type /Pages /Kids [ %s ] /Count %d /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R >> >> >>",
		kids.String(), numPages, pdfPageW, pdfPageH)))

	// obj3 Font（标准 14 之一 Helvetica，不内嵌；WinAnsiEncoding）
	objs = append(objs, []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"))

	// 各页：Page 对象 + Content 流对象
	for i := 0; i < numPages; i++ {
		contentObjNum := 5 + 2*i
		objs = append(objs, []byte(fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /Contents %d 0 R >>", contentObjNum)))

		stream := buildPDFPageContent(table, pages[i], i, numPages, colX, maxChars, titleY, headerY, firstRowY)
		var cb bytes.Buffer
		fmt.Fprintf(&cb, "<< /Length %d >>\nstream\n", len(stream))
		cb.Write(stream)
		cb.WriteString("\nendstream")
		objs = append(objs, cb.Bytes())
	}

	// 组装：header → 对象（记录每个对象起始字节偏移）→ xref → trailer → %%EOF。
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("%\xe2\xe3\xcf\xd3\n") // 二进制标记注释行（提示阅读器这是二进制 PDF）

	offsets := make([]int, len(objs)+1) // 1-indexed：offsets[n] = 对象 n 的 "n 0 obj" 起始偏移
	for k, body := range objs {
		objNum := k + 1
		offsets[objNum] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", objNum)
		buf.Write(body)
		buf.WriteString("\nendobj\n")
	}

	// xref：每条目恰 20 字节（10 位偏移 + 空格 + 5 位代数 + 空格 + 类型 + CRLF）。
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f\r\n") // 对象 0：自由表头
	for objNum := 1; objNum <= len(objs); objNum++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[objNum])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xrefOffset)

	return buf.Bytes(), nil
}

// paginateRows 把数据行按每页 rowsPerPage 行切片；零行返回一页（nil 切片）以仍产出标题/表头页。
func paginateRows(rows [][]string, rowsPerPage int) [][][]string {
	if len(rows) == 0 {
		return [][][]string{nil}
	}
	var pages [][][]string
	for start := 0; start < len(rows); start += rowsPerPage {
		end := start + rowsPerPage
		if end > len(rows) {
			end = len(rows)
		}
		pages = append(pages, rows[start:end])
	}
	return pages
}

// buildPDFPageContent 生成单页内容流：标题 + 页码 + 表头 + 分隔线 + 数据行。
// 文本用自包含 BT/F1 size Tf x y Td (text) Tj ET（绝对定位）；画线用页面空间 m/l/S（在文本对象外）。
func buildPDFPageContent(
	table reportExportTable, rows [][]string, pageIndex, pageCount int,
	colX []float64, maxChars []int, titleY, headerY, firstRowY float64,
) []byte {
	var b bytes.Buffer

	writeText := func(size int, x, y float64, raw string) {
		fmt.Fprintf(&b, "BT /F1 %d Tf %.2f %.2f Td (%s) Tj ET\n", size, x, y, pdfEscapeText(raw))
	}
	writeLine := func(x1, y1, x2, y2 float64) {
		fmt.Fprintf(&b, "%.2f %.2f m %.2f %.2f l S\n", x1, y1, x2, y2)
	}

	b.WriteString("0.5 w\n") // 线宽

	// 标题 + 右上角页码
	writeText(pdfTitleSize, pdfMarginX, titleY, table.Title)
	writeText(pdfHeaderSize, pdfPageW-pdfMarginX-90, titleY,
		fmt.Sprintf("Page %d / %d", pageIndex+1, pageCount))
	writeLine(pdfMarginX, titleY-6, pdfPageW-pdfMarginX, titleY-6)

	// 表头
	for i, h := range table.Headers {
		writeText(pdfHeaderSize, colX[i], headerY, truncateForCol(h, maxChars[i]))
	}
	writeLine(pdfMarginX, headerY-4, pdfPageW-pdfMarginX, headerY-4)

	// 数据行（空集时给占位提示）
	if len(rows) == 0 {
		writeText(pdfDataSize, pdfMarginX, firstRowY, "No records in range.")
		return b.Bytes()
	}
	y := firstRowY
	for _, row := range rows {
		for i := 0; i < len(table.Headers) && i < len(row); i++ {
			cell := row[i]
			if cell == "" {
				continue
			}
			writeText(pdfDataSize, colX[i], y, truncateForCol(cell, maxChars[i]))
		}
		y -= pdfRowH
	}
	return b.Bytes()
}

// truncateForCol 按列可容字符数对原文（rune 计）做截断；超出以 '>' 收尾。
func truncateForCol(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return string(rs[:1])
	}
	return string(rs[:max-1]) + ">"
}

// pdfEscapeText 转义 PDF 文本字符串：'\' '(' ')' 反斜杠转义；制表/换行折叠为空格；
// 仅保留可打印 ASCII（0x20..0x7E），其余（含中文等非拉丁字形）ASCII-fold 为 '?'
// ——标准 14 Helvetica(WinAnsiEncoding) 无法呈现这些字形，CSV 为无损导出。
func pdfEscapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString("\\\\")
		case r == '(':
			b.WriteString("\\(")
		case r == ')':
			b.WriteString("\\)")
		case r == '\t', r == '\r', r == '\n':
			b.WriteByte(' ')
		case r >= 0x20 && r <= 0x7E:
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
