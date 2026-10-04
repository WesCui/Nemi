// Package connectors contains narrowly scoped, outbound official-platform adapters.
package connectors

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var Names = map[string]string{"feishu": "飞书", "wecom": "企业微信", "dingtalk": "钉钉"}

type Bot struct{ URL, Secret, Label string }
type Status struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
	State string `json:"state"`
}
type Sender struct {
	Bots   map[string]Bot
	Client *http.Client
}

func LoadEnv() map[string]Bot {
	bots := map[string]Bot{}
	for id := range Names {
		prefix := "CONNECTOR_" + strings.ToUpper(id)
		bots[id] = Bot{os.Getenv(prefix + "_WEBHOOK"), os.Getenv(prefix + "_SECRET"), os.Getenv(prefix + "_GROUP_NAME")}
	}
	return bots
}
func New(bots map[string]Bot) *Sender {
	return &Sender{bots, &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// A webhook is a credential, not a user-selectable HTTP destination.
func Valid(id string, b Bot) bool {
	u, e := url.Parse(b.URL)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" || u.RawPath != "" || strings.TrimSpace(b.Label) == "" || len([]rune(b.Label)) > 60 {
		return false
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return false
	}
	switch id {
	case "feishu":
		token := strings.TrimPrefix(u.Path, "/open-apis/bot/v2/hook/")
		return u.Host == "open.feishu.cn" && token != u.Path && token != "" && !strings.Contains(token, "/") && len(q) == 0
	case "wecom":
		return u.Host == "qyapi.weixin.qq.com" && u.Path == "/cgi-bin/webhook/send" && len(q) == 1 && len(q["key"]) == 1 && q.Get("key") != ""
	case "dingtalk":
		return u.Host == "oapi.dingtalk.com" && u.Path == "/robot/send" && len(q) == 1 && len(q["access_token"]) == 1 && q.Get("access_token") != "" && b.Secret != ""
	}
	return false
}
func (s *Sender) Statuses() []Status {
	out := []Status{}
	for _, id := range []string{"feishu", "wecom", "dingtalk"} {
		b := s.Bots[id]
		state, label := "unconfigured", ""
		if b.URL != "" {
			state = "invalid"
			if Valid(id, b) {
				state = "configured"
				label = b.Label
			}
		}
		out = append(out, Status{id, Names[id], label, state})
	}
	return out
}
func Sign(id, secret string, now time.Time) (string, string) {
	timestamp := strconv.FormatInt(now.Unix(), 10)
	key, message := timestamp+"\n"+secret, ""
	if id == "dingtalk" {
		timestamp = strconv.FormatInt(now.UnixMilli(), 10)
		key = secret
		message = timestamp + "\n" + secret
	}
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(message))
	return timestamp, base64.StdEncoding.EncodeToString(h.Sum(nil))
}
func (s *Sender) Send(ctx context.Context, id, text string) string {
	b := s.Bots[id]
	if !Valid(id, b) {
		return "REJECTED"
	}
	u, _ := url.Parse(b.URL)
	body := map[string]any{"msgtype": "text", "text": map[string]string{"content": text}}
	if id == "feishu" {
		body = map[string]any{"msg_type": "text", "content": map[string]string{"text": text}}
		if b.Secret != "" {
			timestamp, sign := Sign(id, b.Secret, time.Now())
			body["timestamp"] = timestamp
			body["sign"] = sign
		}
	} else if id == "dingtalk" {
		timestamp, sign := Sign(id, b.Secret, time.Now())
		q := u.Query()
		q.Set("timestamp", timestamp)
		q.Set("sign", sign)
		u.RawQuery = q.Encode()
	}
	raw, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(raw))
	if e != nil {
		return "REJECTED"
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, e := s.Client.Do(req)
	// Never return or log upstream errors: URL errors contain webhook credentials.
	if e != nil {
		return "UNKNOWN"
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "UNKNOWN"
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, 8193))
	if e != nil || len(data) > 8192 {
		return "UNKNOWN"
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) != nil {
		return "UNKNOWN"
	}
	field := "errcode"
	if id == "feishu" {
		field = "code"
		if _, ok := result[field]; !ok {
			field = "StatusCode"
		}
	}
	value, ok := result[field]
	var code *int
	if !ok || json.Unmarshal(value, &code) != nil || code == nil {
		return "UNKNOWN"
	}
	if *code != 0 {
		return "REJECTED"
	}
	return "DELIVERED"
}
