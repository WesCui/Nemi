package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/files"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func TestFilesUploadIdempotencyChatScopeAndCancelledWrites(t *testing.T) {
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		t.Skip("dedicated database not configured")
	}
	if !strings.Contains(db, "/nemi_test?") {
		t.Fatal("dedicated database required")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Pool.Close()
	if err = st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ws := domain.ID()
	code := domain.ID()
	st.Bootstrap(ctx, code, ws)
	token, _ := st.Login(ctx, code)
	v, _ := vault.New(bytes.Repeat([]byte{27}, 32))
	fs, err := files.New(config.Config{FilesRoot: t.TempDir()}, st, v)
	if err != nil {
		t.Fatal(err)
	}
	fs.Parse = func(ctx context.Context, name string, data []byte) (domain.FileContent, error) {
		return files.Parse(name, data)
	}
	g := model.New(config.Config{Provider: "deepseek", Model: "fixture", Key: "test-key", InputPrice: 2000000, OutputPrice: 4000000})
	h := (&API{Store: st, Vault: v, Files: fs, Gateway: g, Config: config.Config{Origin: "http://localhost:3000"}}).Handler()
	call := func(method, path, ctype string, data []byte, key, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", ctype)
		r.Header.Set("Origin", "http://localhost:3000")
		r.Header.Set("Idempotency-Key", key)
		r.AddCookie(&http.Cookie{Name: "nemi_session", Value: cookie})
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	p, _ := m.CreateFormFile("file", "账单.csv")
	p.Write([]byte("类型,金额\n餐饮,0.1\n餐饮,0.2\n"))
	m.Close()
	key := domain.ID()
	up := call("POST", "/api/v1/files", m.FormDataContentType(), body.Bytes(), key, token)
	if up.Code != 201 {
		t.Fatal("upload", up.Code)
	}
	var f domain.File
	if json.Unmarshal(up.Body.Bytes(), &f) != nil {
		t.Fatal("invalid upload")
	}
	duplicate := call("POST", "/api/v1/files", m.FormDataContentType(), body.Bytes(), key, token)
	var duplicateFile domain.File
	if duplicate.Code != 201 || json.Unmarshal(duplicate.Body.Bytes(), &duplicateFile) != nil || duplicateFile.ID != f.ID {
		t.Fatal("duplicate upload")
	}
	bad := call("POST", "/api/v1/chat/messages", "application/json", []byte(`{"text":"读取资料","file_ids":["`+domain.ID()+`"]}`), domain.ID(), token)
	if bad.Code != 422 {
		t.Fatal("invalid attachment admitted", bad.Code)
	}
	created := call("POST", "/api/v1/chat/messages", "application/json", []byte(`{"text":"统计账单","file_ids":["`+f.ID+`"]}`), domain.ID(), token)
	if created.Code != 202 {
		t.Fatal(created.Code)
	}
	var chat struct {
		Run          string `json:"run_id"`
		Conversation string `json:"conversation_id"`
	}
	json.Unmarshal(created.Body.Bytes(), &chat)
	ref := store.Ref{Workspace: ws, ID: chat.Run}
	if _, err = st.AgentFile(ctx, ref, f.ID); err != nil {
		t.Fatal("attached file unavailable", err)
	}
	other := call("POST", "/api/v1/chat/messages", "application/json", []byte(`{"text":"另一段对话"}`), domain.ID(), token)
	var second struct {
		Run string `json:"run_id"`
	}
	json.Unmarshal(other.Body.Bytes(), &second)
	if _, err = st.AgentFile(ctx, store.Ref{Workspace: ws, ID: second.Run}, f.ID); err == nil {
		t.Fatal("file leaked across chats")
	}
	otherWS := domain.ID()
	otherCode := domain.ID()
	st.Bootstrap(ctx, otherCode, otherWS)
	otherToken, _ := st.Login(ctx, otherCode)
	if got := call("GET", "/api/v1/files/"+f.ID+"/download", "", nil, "", otherToken); got.Code != 404 {
		t.Fatal("cross-owner download", got.Code)
	}
	preview := call("GET", "/api/v1/files/"+f.ID+"/preview", "", nil, "", token)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "0.2") {
		t.Fatal("preview", preview.Code)
	}
	download := call("GET", "/api/v1/files/"+f.ID+"/download", "", nil, "", token)
	if download.Code != 200 || !strings.Contains(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("download", download.Code)
	}
	g = g.ForKind("chat")
	if _, err = st.AdmitRun(ctx, ref, g.Reserve); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ClaimModel(ctx, ref, g.Profile()); err != nil {
		t.Fatal(err)
	}
	stop := call("POST", "/api/v1/chat/runs/"+ref.ID+"/stop", "application/json", []byte(`{}`), domain.ID(), token)
	if stop.Code != 200 {
		t.Fatal("stop", stop.Code)
	}
	c, _ := files.Parse("结果.txt", []byte("不得保存"))
	_, err = st.Command(ctx, ws, domain.ID(), "cancelled-file", []byte(`{}`), func(tx pgx.Tx) (any, int, error) {
		result, err := fs.Save(ctx, tx, ws, &ref, "结果.txt", "artifact", "", []byte("不得保存"), c)
		return result, 201, err
	})
	if err == nil {
		t.Fatal("cancelled run wrote artifact")
	}
}
