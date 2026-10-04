package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"nemi/internal/config"
	"nemi/internal/files"
	"nemi/internal/model"
	"nemi/internal/store"
	"nemi/internal/vault"
)

// Compiled only into the Go test executable, never any product binary. The
// browser suite exercises real PG/Temporal plus this provider protocol fixture.
func TestE2EWorkerService(t *testing.T) {
	if os.Getenv("NEMI_E2E_WORKER") != "1" {
		t.Skip("isolated browser worker only")
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal("invalid isolated worker config")
	}
	if !strings.Contains(c.DB, "/nemi_test?") || !strings.HasPrefix(c.RunQueue, "nemi-e2e-") {
		t.Fatal("dedicated database and queue required")
	}
	s, err := store.Open(context.Background(), c.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Pool.Close()
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(c.VaultKey, c.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	g := model.New(c)
	fs, err := files.New(c, s, v)
	if err != nil {
		t.Fatal(err)
	}
	g.HTTP.Transport = personalTransport(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Messages []struct{ Role, Content string }
			Model    string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		content := `{"summary":"核对出行资料","items":["确认出发时间、同行人数与预算","核实开放时间与交通","准备证件和充电设备"]}`
		var toolCall any
		if strings.Contains(body.Messages[0].Content, "会话整理器") {
			var snapshot struct {
				Previous string                           `json:"previous_summary"`
				History  []struct{ Role, Content string } `json:"completed_history"`
			}
			if err := json.Unmarshal([]byte(body.Messages[1].Content), &snapshot); err != nil {
				return nil, err
			}
			content = snapshot.Previous
			if content == "" {
				for _, m := range snapshot.History {
					if m.Role == "user" {
						content = "历史约束：" + m.Content
						break
					}
				}
			}
		} else if !strings.Contains(body.Messages[0].Content, "仅输出 JSON") {
			last := body.Messages[len(body.Messages)-1].Content
			userText := ""
			for _, message := range body.Messages {
				if message.Role == "user" {
					userText = message.Content
				}
			}
			dataName, dataArgs, dataContent, dataWorkflow, dataErr := dataFixture(userText, body.Messages[len(body.Messages)-1].Role, last)
			if dataErr != nil {
				return nil, dataErr
			}
			continuationName, continuationArgs, continuationContent, continuationWorkflow, continuationErr := continuationFixture(userText, body.Messages[len(body.Messages)-1].Role, last)
			if continuationErr != nil {
				return nil, continuationErr
			}
			fileWorkflow := false
			for _, m := range body.Messages {
				if m.Role == "user" && m.Content == "统计这份账单并生成Excel汇总" {
					fileWorkflow = true
				}
			}
			if last == "执行可停止的任务" {
				select {
				case <-r.Context().Done():
					return nil, r.Context().Err()
				case <-time.After(15 * time.Second):
				}
			}
			if strings.Contains(last, "触发认证失败") {
				return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture authentication failed"}}`))}, nil
			}
			if continuationWorkflow {
				content = continuationContent
				if continuationName != "" {
					toolCall = map[string]any{"id": "fixture_continuation_" + continuationName, "type": "function", "function": map[string]string{"name": continuationName, "arguments": continuationArgs}}
				}
			} else if dataWorkflow {
				content = dataContent
				if dataName != "" {
					toolCall = map[string]any{"id": "fixture_data_" + dataName, "type": "function", "function": map[string]string{"name": dataName, "arguments": dataArgs}}
				}
			} else if fileWorkflow {
				name, args := "list_files", "{}"
				if body.Messages[len(body.Messages)-1].Role == "tool" {
					var value map[string]json.RawMessage
					if json.Unmarshal([]byte(last), &value) != nil {
						return nil, fmt.Errorf("invalid file tool output")
					}
					if raw, ok := value["files"]; ok {
						var list []struct {
							ID string `json:"id"`
						}
						json.Unmarshal(raw, &list)
						if len(list) == 0 {
							return nil, fmt.Errorf("no attached file")
						}
						name = "analyze_table"
						args = fmt.Sprintf(`{"file_id":%q,"table":0,"column":1,"has_header":true}`, list[0].ID)
					} else if raw, ok := value["groups"]; ok {
						var groups []struct {
							Sum string `json:"sum"`
						}
						json.Unmarshal(raw, &groups)
						if len(groups) != 1 {
							return nil, fmt.Errorf("unexpected statistics")
						}
						name = "create_artifact"
						encoded, _ := json.Marshal(map[string]string{"name": "生活账单汇总.xlsx", "content": "项目,金额\n合计," + groups[0].Sum + "\n"})
						args = string(encoded)
					} else if _, ok := value["id"]; ok {
						name = ""
						content = "已统计完整账单并生成 Excel 汇总，请在对话中预览或下载。"
					} else {
						return nil, fmt.Errorf("file workflow failed")
					}
				}
				if name != "" {
					toolCall = map[string]any{"id": "fixture_file_" + name, "type": "function", "function": map[string]string{"name": name, "arguments": args}}
				}
			} else if body.Messages[len(body.Messages)-1].Role == "tool" {
				if strings.Contains(last, `"progress_only":true`) {
					content = "已经记录工作计划。"
				} else if strings.Contains(last, `"status":"PENDING"`) {
					content = "已准备好提案，请确认后创建。"
				} else {
					content = "事项查询结果：" + last
				}
			} else if last == "连接企业微信" {
				toolCall = map[string]any{"id": "fixture_connect", "type": "function", "function": map[string]string{"name": "request_connection", "arguments": `{"app_id":"wecom"}`}}
			} else if last == "发送群消息" {
				toolCall = map[string]any{"id": "fixture_message", "type": "function", "function": map[string]string{"name": "propose_message", "arguments": `{"channel_id":"wecom","text":"请核对这份工作安排。"}`}}
			} else if last == "制定任务计划" {
				toolCall = map[string]any{"id": "fixture_plan", "type": "function", "function": map[string]string{"name": "update_plan", "arguments": `{"goal":"规划周末出行","steps":[{"title":"核对预算","status":"completed"},{"title":"比较方案","status":"in_progress"},{"title":"交付建议","status":"pending"}]}`}}
			} else if strings.HasPrefix(last, "帮我创建一个事项：") {
				at := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
				args, _ := json.Marshal(map[string]any{"title": strings.TrimPrefix(last, "帮我创建一个事项："), "category": "life", "source": "收好物品并整理书桌", "reminder_at": at, "repeat": "once"})
				toolCall = map[string]any{"id": "fixture_proposal", "type": "function", "function": map[string]string{"name": "propose_matter", "arguments": string(args)}}
			} else if last == "查一下我的事项" {
				toolCall = map[string]any{"id": "fixture_list", "type": "function", "function": map[string]string{"name": "list_matters", "arguments": "{}"}}
			} else {
				content = "收到：" + last
			}
			if !fileWorkflow && !dataWorkflow && !continuationWorkflow && len(body.Messages) > 2 && body.Messages[len(body.Messages)-1].Role != "tool" {
				content += "；前文：" + body.Messages[1].Content
			}
		}
		choice := map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}
		if toolCall != nil {
			choice = map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"tool_calls": []any{toolCall}}}
		}
		wire, _ := json.Marshal(map[string]any{"choices": []any{choice}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 30}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(wire)))}, nil
	})
	engine, err := client.Dial(client.Options{HostPort: c.Temporal})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	w := worker.New(engine, c.RunQueue, worker.Options{})
	w.RegisterWorkflow(RunWorkflow)
	w.RegisterWorkflow(ContinuationWorkflow)
	w.RegisterActivity(&Activities{Store: s, Gateway: g, Vault: v, Files: fs})
	if err = w.Run(worker.InterruptCh()); err != nil {
		t.Fatal(err)
	}
}

func continuationFixture(user, role, last string) (name, args, content string, handled bool, err error) {
	prefix := "先保存事项再整理报告："
	title := strings.TrimPrefix(user, prefix)
	resume := false
	if !strings.HasPrefix(user, prefix) {
		marker := "继续既定目标：保存事项："
		index := strings.Index(user, marker)
		if index < 0 {
			return
		}
		resume = true
		title = strings.Split(user[index+len(marker):], "\n")[0]
	}
	handled = true
	encode := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if role != "tool" {
		if resume {
			name = "list_matters"
			args = encode(map[string]string{"query": title})
		} else {
			name = "propose_matter"
			args = encode(map[string]string{"title": title, "category": "life", "source": "请在保存后核对资料并整理报告"})
		}
		return
	}
	var list []map[string]any
	if json.Unmarshal([]byte(last), &list) == nil {
		if len(list) != 1 {
			err = fmt.Errorf("expected saved matter")
			return
		}
		name = "get_matter"
		args = encode(map[string]any{"id": list[0]["id"]})
		return
	}
	var value map[string]any
	if json.Unmarshal([]byte(last), &value) != nil {
		err = fmt.Errorf("invalid continuation output")
		return
	}
	if !resume && value["status"] == "PENDING" {
		name = "await_actions"
		args = encode(store.WaitForActions{Goal: "保存事项：" + title, Next: "核对刚才保存的事项，然后生成文字报告", Actions: []string{value["id"].(string)}})
		return
	}
	if resume && value["title"] != nil {
		name = "create_artifact"
		args = encode(map[string]string{"name": title + ".md", "content": "# " + value["title"].(string) + "\n\n已保存状态：" + value["status"].(string) + "\n\n" + value["source"].(string)})
		return
	}
	if resume && value["kind"] == "artifact" {
		content = "已核对保存结果，并生成文字报告。"
		return
	}
	err = fmt.Errorf("continuation workflow did not receive expected state")
	return
}

// The test provider derives IDs and revisions from actual tool responses. This
// drives PG/Temporal/UI contracts without any external account or paid model.
func dataFixture(user, role, last string) (name, args, content string, handled bool, err error) {
	op, query := "", ""
	for prefix, operation := range map[string]string{"把事项清单第一项标记完成：": "checklist", "归档事项：": "archive", "停用事项提醒：": "reminder", "记住我的偏好：": "save", "移除这条偏好：": "delete"} {
		if strings.HasPrefix(user, prefix) {
			op, query = operation, strings.TrimPrefix(user, prefix)
			break
		}
	}
	if op == "" {
		return
	}
	handled = true
	encode := func(value any) string { b, _ := json.Marshal(value); return string(b) }
	if role != "tool" {
		if op == "save" {
			name = "propose_memory"
			args = encode(map[string]any{"operation": "save", "expected_revision": 0, "category": "life", "text": query})
		} else if op == "delete" {
			name = "list_memories"
			args = encode(map[string]any{"query": query})
		} else {
			name = "list_matters"
			args = encode(map[string]any{"query": query})
		}
		return
	}
	var list []map[string]any
	if json.Unmarshal([]byte(last), &list) == nil {
		if len(list) != 1 {
			err = fmt.Errorf("expected one matching data record")
			return
		}
		if op == "delete" {
			name = "propose_memory"
			args = encode(map[string]any{"operation": "delete", "memory_id": list[0]["id"], "expected_revision": list[0]["revision"]})
		} else if op == "reminder" {
			name = "get_reminder"
			args = encode(map[string]any{"matter_id": list[0]["id"]})
		} else {
			name = "get_matter"
			args = encode(map[string]any{"id": list[0]["id"]})
		}
		return
	}
	var value map[string]any
	if json.Unmarshal([]byte(last), &value) != nil {
		err = fmt.Errorf("invalid data tool output")
		return
	}
	if value["status"] == "PENDING" {
		content = "已准备好修改，请核对后确认。"
		return
	}
	if op == "reminder" {
		r, ok := value["reminder"].(map[string]any)
		if !ok {
			err = fmt.Errorf("no reminder")
			return
		}
		name = "propose_reminder_update"
		args = encode(map[string]any{"matter_id": value["matter_id"], "matter_revision": value["matter_revision"], "expected_revision": r["revision"], "enabled": false})
		return
	}
	if op == "checklist" || op == "archive" {
		p := map[string]any{"matter_id": value["id"], "expected_revision": value["revision"]}
		if op == "checklist" {
			p["checklist_changes"] = []any{map[string]any{"index": 0, "done": true}}
		} else {
			p["status"] = "ARCHIVED"
		}
		name = "propose_matter_update"
		args = encode(p)
		return
	}
	err = fmt.Errorf("unexpected data tool response")
	return
}
