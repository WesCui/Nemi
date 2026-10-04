package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"io"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type personalTransport func(*http.Request) (*http.Response, error)

func (f personalTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPersonalModelRunAcrossRealTemporalAndDefaultChange(t *testing.T) {
	db, addr := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_TEMPORAL_ADDRESS")
	if db == "" || addr == "" {
		t.Skip("isolated integration environment not configured")
	}
	if !strings.Contains(db, "/nemi_test?") {
		t.Fatal("dedicated database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Pool.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ws := "personal-test-" + domain.ID()
	if _, err = s.Pool.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1)", ws); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(bytes.Repeat([]byte{19}, 32))
	m := store.PersonalModel{ID: domain.ID(), Label: "task-specific fixture", Provider: "kimi", Model: "kimi-k2.6", InputPrice: 2000000, OutputPrice: 4000000}
	m.Credential, err = v.Seal(model.CredentialOwner(ws, m.ID), []byte("test-only-personal-key"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := model.Personal(m, ws, v)
	if err != nil {
		t.Fatal(err)
	}
	cmd := func(route string, fn func(pgx.Tx) (any, int, error)) store.CommandResult {
		t.Helper()
		out, e := s.Command(ctx, ws, domain.ID(), route, nil, fn)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	cmd("save", func(tx pgx.Tx) (any, int, error) { return s.AddModel(ctx, tx, ws, m) })
	cmd("default", func(tx pgx.Tx) (any, int, error) { return s.DefaultModel(ctx, tx, ws, m.ID) })
	out := cmd("matter", func(tx pgx.Tx) (any, int, error) {
		return s.CreateMatter(ctx, tx, ws, domain.CreateMatter{Title: "测试个人模型", Category: "life"})
	})
	var matter domain.Matter
	json.Unmarshal(out.Body, &matter)
	out = cmd("run", func(tx pgx.Tx) (any, int, error) {
		out, status, e := s.CreateRun(ctx, tx, ws, matter.ID, g.Mode(), g.Profile(), 1)
		if e == nil {
			e = s.BindRunModel(ctx, tx, ws, out.(domain.Run).ID, m.ID)
		}
		return out, status, e
	})
	var run domain.Run
	json.Unmarshal(out.Body, &run)
	cmd("change-default", func(tx pgx.Tx) (any, int, error) { return s.DefaultModel(ctx, tx, ws, "") })
	fallback := model.New(config.Config{Provider: "demo"})
	var calls atomic.Int32
	fallback.HTTP.Transport = personalTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "api.moonshot.cn" || r.Header.Get("Authorization") != "Bearer test-only-personal-key" {
			t.Error("run used another provider or credential")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"kimi-k2.6"`) {
			t.Error("run lost selected model")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"连接正常\",\"items\":[\"核对时间\",\"准备资料\",\"记录结果\"]}"}}],"usage":{"prompt_tokens":50,"completion_tokens":30}}`))}, nil
	})
	tc, err := client.Dial(client.Options{HostPort: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer tc.Close()
	queue := "nemi-personal-test-" + domain.ID()
	w := worker.New(tc, queue, worker.Options{})
	w.RegisterWorkflow(RunWorkflow)
	w.RegisterActivity(&Activities{Store: s, Gateway: fallback, Vault: v})
	if err = w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	execution, err := tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: "test/" + ws + "/run", TaskQueue: queue}, RunWorkflow, store.Ref{Workspace: ws, ID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = execution.Get(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var status, mode string
	var charged int64
	err = s.Pool.QueryRow(ctx, "SELECT status,mode,charged_micro_cny FROM runs WHERE workspace_id=$1 AND id=$2", ws, run.ID).Scan(&status, &mode, &charged)
	if err != nil || status != "SUCCEEDED" || mode != "personal" || charged != 220 || calls.Load() != 1 {
		t.Fatal("durable personal model execution failed", err, status, charged, calls.Load())
	}
	var verified bool
	s.Pool.QueryRow(ctx, "SELECT verified_at IS NOT NULL FROM personal_models WHERE workspace_id=$1 AND id=$2", ws, m.ID).Scan(&verified)
	if !verified {
		t.Fatal("successful generation wasn't recorded as verified")
	}
}
