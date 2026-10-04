package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"nemi/internal/files"
)

func fileError(w http.ResponseWriter, err error) {
	messages := map[string]string{
		"FILE_FORMAT_UNSUPPORTED":         "支持 PDF、Word（docx）、Excel（xlsx）、CSV、TXT、Markdown 和 JSON 文件",
		"FILE_NAME_INVALID":               "文件名称无效，请修改名称后重试",
		"FILE_TOO_LARGE":                  "单个文件最多 5 MB；解析内容最多 256 KB，PDF 最多 50 页，表格最多 2 万个单元格。请提供需要的部分。",
		"FILE_EMPTY_OR_SCANNED":           "没有读到文字或表格；扫描 PDF 需要先转为可选中文字的文件",
		"FILE_ENCODING_UNSUPPORTED":       "文本文件请保存为 UTF-8 编码",
		"FILE_INVALID_OR_ENCRYPTED":       "文件无法读取，请先解除密码或换一个文件",
		"FILE_ACTIVE_CONTENT_UNSUPPORTED": "暂不读取包含宏的文件，请另存为普通文档",
		"FILE_INVALID":                    "文件内容或格式无法读取",
		"FILE_QUOTA_EXCEEDED":             "已达到资料空间上限（200 个文件或 100 MB）",
	}
	if errors.Is(err, domain.ErrNotFound) {
		sendError(w, 404, "找不到这份资料")
		return
	}
	if msg := messages[err.Error()]; msg != "" {
		sendError(w, 422, msg)
		return
	}
	sendError(w, 503, "暂时无法处理文件，请稍后重试")
}
func (a *API) uploadFile(w http.ResponseWriter, r *http.Request) {
	if a.Files == nil {
		sendError(w, 503, "文件存储尚未配置")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, files.MaxBytes+16<<10)
	m, err := r.MultipartReader()
	if err != nil {
		sendError(w, 400, "请上传一个文件")
		return
	}
	p, err := m.NextPart()
	if err != nil || p.FormName() != "file" {
		sendError(w, 400, "请上传一个文件")
		return
	}
	name := p.FileName()
	if _, err = files.MIME(name); err != nil {
		fileError(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(p, files.MaxBytes+1))
	p.Close()
	if err != nil || len(data) > files.MaxBytes {
		fileError(w, errors.New("FILE_TOO_LARGE"))
		return
	}
	if _, err = m.NextPart(); err != io.EOF {
		sendError(w, 400, "每次上传一个文件")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 100 {
		sendError(w, 400, "缺少有效的请求标识")
		return
	}
	content, err := a.Files.Parse(r.Context(), name, data)
	if err != nil {
		fileError(w, err)
		return
	}
	hash := sha256.Sum256(data)
	body := []byte(name + ":" + hex.EncodeToString(hash[:]))
	result, err := a.Store.Command(r.Context(), identity(r).Workspace, key, "POST /api/v1/files", body, func(tx pgx.Tx) (any, int, error) {
		f, err := a.Files.Save(r.Context(), tx, identity(r).Workspace, nil, name, "upload", "", data, content)
		return f, 201, err
	})
	if errors.Is(err, domain.ErrConflict) {
		sendError(w, 409, "请求标识已使用，请重新上传")
		return
	}
	if err != nil {
		fileError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(result.Status)
	w.Write(result.Body)
}
func (a *API) fileContent(w http.ResponseWriter, r *http.Request) {
	if a.Files == nil {
		sendError(w, 503, "文件存储尚未配置")
		return
	}
	f, err := a.Store.File(r.Context(), identity(r).Workspace, r.PathValue("id"))
	if err != nil {
		fileError(w, err)
		return
	}
	data, content, err := a.Files.Read(r.Context(), identity(r).Workspace, f)
	if err != nil {
		fileError(w, err)
		return
	}
	if r.PathValue("view") == "download" {
		w.Header().Set("Content-Type", f.MIME)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
		return
	}
	send(w, 200, map[string]any{"file": f.File, "content": content})
}
