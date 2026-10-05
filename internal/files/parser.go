// Package files reads passive documents only. It never executes macros,
// formulas, embedded programs, or links from uploaded content.
package files

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
	"nemi/internal/domain"
)

const MaxBytes = 5 << 20
const MaxContent = 256 << 10

func validName(name string) bool {
	if len(name) == 0 || len(name) > 240 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func MIME(name string) (string, error) {
	if !validName(name) {
		return "", errors.New("FILE_NAME_INVALID")
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt":
		return "text/plain; charset=utf-8", nil
	case ".md":
		return "text/markdown; charset=utf-8", nil
	case ".json":
		return "application/json", nil
	case ".csv":
		return "text/csv; charset=utf-8", nil
	case ".ics":
		return "text/calendar; charset=utf-8", nil
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	case ".pdf":
		return "application/pdf", nil
	default:
		return "", errors.New("FILE_FORMAT_UNSUPPORTED")
	}
}

func Validate(c domain.FileContent) error {
	if !utf8.ValidString(c.Text) {
		return errors.New("FILE_ENCODING_UNSUPPORTED")
	}
	cells := 0
	if len(c.Tables) > 25 {
		return errors.New("FILE_TOO_LARGE")
	}
	for _, t := range c.Tables {
		if len(t.Rows) > 5000 {
			return errors.New("FILE_TOO_LARGE")
		}
		for _, row := range t.Rows {
			if len(row) > 100 {
				return errors.New("FILE_TOO_LARGE")
			}
			cells += len(row)
			for _, cell := range row {
				if len(cell) > 4000 || !utf8.ValidString(cell) {
					return errors.New("FILE_TOO_LARGE")
				}
			}
		}
	}
	b, _ := json.Marshal(c)
	if len(b) > MaxContent || cells > 20000 {
		return errors.New("FILE_TOO_LARGE")
	}
	if strings.TrimSpace(c.Text) == "" && cells == 0 {
		return errors.New("FILE_EMPTY_OR_SCANNED")
	}
	return nil
}

func Parse(name string, data []byte) (domain.FileContent, error) {
	c := domain.FileContent{Tables: []domain.Table{}}
	if _, err := MIME(name); err != nil {
		return c, err
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return c, errors.New("FILE_TOO_LARGE")
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".json":
		if !utf8.Valid(data) {
			return c, errors.New("FILE_ENCODING_UNSUPPORTED")
		}
		if strings.HasSuffix(strings.ToLower(name), ".json") && !json.Valid(data) {
			return c, errors.New("FILE_INVALID")
		}
		c.Text = strings.TrimPrefix(string(data), "\uFEFF")
	case ".csv":
		if !utf8.Valid(data) {
			return c, errors.New("FILE_ENCODING_UNSUPPORTED")
		}
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\uFEFF")))
		r.FieldsPerRecord = -1
		rows := [][]string{}
		cells := 0
		for {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return c, errors.New("FILE_INVALID")
			}
			cells += len(row)
			if len(row) > 100 || len(rows) >= 5000 || cells > 20000 {
				return c, errors.New("FILE_TOO_LARGE")
			}
			rows = append(rows, row)
		}
		c.Tables = append(c.Tables, domain.Table{Name: "数据", Rows: rows})
	case ".docx":
		z, err := checkedZIP(data)
		if err != nil {
			return c, err
		}
		var document *zip.File
		for _, f := range z.File {
			if f.Name == "word/document.xml" {
				document = f
			}
		}
		if document == nil {
			return c, errors.New("FILE_INVALID")
		}
		r, err := document.Open()
		if err != nil {
			return c, errors.New("FILE_INVALID")
		}
		defer r.Close()
		d := xml.NewDecoder(io.LimitReader(r, 10<<20))
		var b strings.Builder
		inside := false
		for {
			t, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return c, errors.New("FILE_INVALID")
			}
			switch x := t.(type) {
			case xml.StartElement:
				if x.Name.Local == "t" {
					inside = true
				}
				if x.Name.Local == "tab" {
					b.WriteString("\t")
				}
				if x.Name.Local == "br" {
					b.WriteString("\n")
				}
			case xml.EndElement:
				if x.Name.Local == "t" {
					inside = false
				}
				if x.Name.Local == "p" || x.Name.Local == "tr" {
					b.WriteString("\n")
				}
			case xml.CharData:
				if inside {
					b.Write(x)
				}
			}
			if b.Len() > MaxContent {
				return c, errors.New("FILE_TOO_LARGE")
			}
		}
		c.Text = b.String()
	case ".xlsx":
		if _, err := checkedZIP(data); err != nil {
			return c, err
		}
		f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true, UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 4 << 20})
		if err != nil {
			return c, errors.New("FILE_INVALID")
		}
		defer f.Close()
		if len(f.GetSheetList()) > 25 {
			return c, errors.New("FILE_TOO_LARGE")
		}
		cells, size := 0, 0
		for _, name := range f.GetSheetList() {
			rows, err := f.Rows(name)
			if err != nil {
				return c, errors.New("FILE_INVALID")
			}
			table := domain.Table{Name: name, Rows: [][]string{}}
			for rows.Next() {
				row, err := rows.Columns(excelize.Options{RawCellValue: true})
				if err != nil {
					rows.Close()
					return c, errors.New("FILE_INVALID")
				}
				cells += len(row)
				for _, cell := range row {
					size += len(cell)
				}
				if len(row) > 100 || len(table.Rows) >= 5000 || cells > 20000 || size > MaxContent {
					rows.Close()
					return c, errors.New("FILE_TOO_LARGE")
				}
				table.Rows = append(table.Rows, row)
			}
			err = rows.Error()
			rows.Close()
			if err != nil {
				return c, errors.New("FILE_INVALID")
			}
			c.Tables = append(c.Tables, table)
		}
	case ".pdf":
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return c, errors.New("FILE_INVALID_OR_ENCRYPTED")
		}
		if r.NumPage() > 50 {
			return c, errors.New("FILE_TOO_LARGE")
		}
		var b strings.Builder
		for i := 1; i <= r.NumPage(); i++ {
			text, err := r.Page(i).GetPlainText(nil)
			if err != nil {
				return c, errors.New("FILE_INVALID_OR_ENCRYPTED")
			}
			b.WriteString(text)
			b.WriteString("\n")
			if b.Len() > MaxContent {
				return c, errors.New("FILE_TOO_LARGE")
			}
		}
		c.Text = b.String()
	}
	return c, Validate(c)
}

func checkedZIP(data []byte) (*zip.Reader, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("FILE_INVALID")
	}
	if len(z.File) > 500 {
		return nil, errors.New("FILE_TOO_LARGE")
	}
	var total uint64
	for _, f := range z.File {
		if f.UncompressedSize64 > 10<<20 {
			return nil, errors.New("FILE_TOO_LARGE")
		}
		total += f.UncompressedSize64
		if total > 32<<20 {
			return nil, errors.New("FILE_TOO_LARGE")
		}
		if strings.Contains(strings.ToLower(f.Name), "vbaproject") {
			return nil, errors.New("FILE_ACTIVE_CONTENT_UNSUPPORTED")
		}
	}
	return z, nil
}
