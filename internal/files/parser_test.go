package files

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPassiveFormatsAndBounds(t *testing.T) {
	for _, name := range []string{"笔记.txt", "报告.md", "资料.json"} {
		text := "你好，妮米"
		if strings.HasSuffix(name, "json") {
			text = `{"message":"你好"}`
		}
		c, err := Parse(name, []byte(text))
		if err != nil || c.Text != text {
			t.Fatal(name, err)
		}
	}
	c, err := Parse("账单.csv", []byte("类型,金额\n餐饮,0.1\n餐饮,0.2\n交通,10\n"))
	if err != nil || len(c.Tables[0].Rows) != 4 {
		t.Fatal(err)
	}
	var doc bytes.Buffer
	z := zip.NewWriter(&doc)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>正文内容</w:t></w:r></w:p></w:body></w:document>`))
	z.Close()
	c, err = Parse("资料.docx", doc.Bytes())
	if err != nil || !strings.Contains(c.Text, "正文内容") {
		t.Fatal(err)
	}
	f := excelize.NewFile()
	defer f.Close()
	f.SetCellStr("Sheet1", "A1", "金额")
	f.SetCellStr("Sheet1", "A2", "0.1")
	f.SetCellStr("Sheet1", "B2", "=HYPERLINK(\"https://invalid.example\")")
	data, _ := f.WriteToBuffer()
	c, err = Parse("表.xlsx", data.Bytes())
	if err != nil || c.Tables[0].Rows[1][1] != `=HYPERLINK("https://invalid.example")` {
		t.Fatal(err)
	}
	for _, v := range []struct {
		name string
		data []byte
	}{{"../secret.txt", []byte("x")}, {"app.exe", []byte("x")}, {"utf.txt", []byte{255}}, {"broken.pdf", []byte("broken")}, {"oversized.txt", bytes.Repeat([]byte("a"), MaxContent+1)}} {
		if _, err = Parse(v.name, v.data); err == nil {
			t.Fatal("accepted invalid input", v.name)
		}
	}
	var bomb bytes.Buffer
	z = zip.NewWriter(&bomb)
	w, _ = z.Create("word/document.xml")
	w.Write(bytes.Repeat([]byte("a"), 11<<20))
	z.Close()
	if _, err = Parse("zip.docx", bomb.Bytes()); err == nil {
		t.Fatal("accepted oversized ZIP")
	}
}
func TestTextPDF(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>`, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`}
	stream := "BT /F1 12 Tf 20 200 Td (Nemi PDF text) Tj ET"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	c, err := Parse("资料.pdf", b.Bytes())
	if err != nil || !strings.Contains(c.Text, "Nemi PDF text") {
		t.Fatal(err, c.Text)
	}
}
