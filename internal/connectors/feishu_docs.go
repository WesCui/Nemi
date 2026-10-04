package connectors

import (
	"context"
	"encoding/json"
	"errors"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkauth "github.com/larksuite/oapi-sdk-go/v3/service/auth/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx"
	larkdocxv1 "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type FeishuApp struct {
	ID     string `json:"app_id"`
	Secret string `json:"app_secret"`
}
type FeishuDocuments struct{ Client *http.Client }

func NewFeishuDocuments() *FeishuDocuments {
	return &FeishuDocuments{Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var docID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func FeishuDocumentID(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawPath != "" || !strings.HasSuffix(u.Hostname(), ".feishu.cn") {
		return "", errors.New("DOCUMENT_URL_INVALID")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "docx" || !docID.MatchString(parts[1]) {
		return "", errors.New("DOCUMENT_URL_INVALID")
	}
	return parts[1], nil
}

type quietSDKLogger struct{}

func (quietSDKLogger) Debug(context.Context, ...interface{}) {}
func (quietSDKLogger) Info(context.Context, ...interface{})  {}
func (quietSDKLogger) Warn(context.Context, ...interface{})  {}
func (quietSDKLogger) Error(context.Context, ...interface{}) {}

type boundedFeishuHTTP struct{ client *http.Client }

func (c boundedFeishuHTTP) Do(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "open.feishu.cn" {
		return nil, errors.New("DOCUMENT_ENDPOINT_INVALID")
	}
	resp, err := c.client.Do(r)
	if err == nil {
		resp.Body = http.MaxBytesReader(nil, resp.Body, 1<<20)
	}
	return resp, err
}
func (f *FeishuDocuments) Read(ctx context.Context, app FeishuApp, id string) (string, error) {
	if !docID.MatchString(id) {
		return "", errors.New("DOCUMENT_URL_INVALID")
	}
	// Reuse the official SDK's auth and docx service without loading unrelated
	// generated services. Token cache is disabled for credential revocation.
	cfg := &larkcore.Config{BaseUrl: "https://open.feishu.cn", AppId: app.ID, AppSecret: app.Secret, AppType: larkcore.AppTypeSelfBuilt, EnableTokenCache: false, Logger: quietSDKLogger{}, HttpClient: boundedFeishuHTTP{f.Client}}
	larkcore.NewLogger(cfg)
	larkcore.NewCache(cfg)
	larkcore.NewSerialization(cfg)
	larkcore.NewHttpClient(cfg)
	tokenResponse, err := larkauth.New(cfg).TenantAccessToken.Internal(ctx, larkauth.NewInternalTenantAccessTokenReqBuilder().Body(larkauth.NewInternalTenantAccessTokenReqBodyBuilder().AppId(app.ID).AppSecret(app.Secret).Build()).Build())
	if err != nil || tokenResponse == nil || tokenResponse.ApiResp == nil || tokenResponse.StatusCode != 200 {
		return "", errors.New("DOCUMENT_AUTH_OR_NETWORK_FAILED")
	}
	// The auth endpoint returns its token at the top level, whereas some SDK
	// generated response types expect data. Validate the official wire envelope.
	var token struct {
		Code   *int   `json:"code"`
		Value  string `json:"tenant_access_token"`
		Expire int    `json:"expire"`
	}
	if err = json.Unmarshal(tokenResponse.RawBody, &token); err != nil || token.Code == nil || *token.Code != 0 || token.Value == "" || token.Expire <= 0 {
		return "", errors.New("DOCUMENT_AUTH_OR_NETWORK_FAILED")
	}
	resp, err := larkdocx.NewService(cfg).Document.RawContent(ctx, larkdocxv1.NewRawContentDocumentReqBuilder().DocumentId(id).Build(), larkcore.WithTenantAccessToken(token.Value))
	if err != nil {
		return "", errors.New("DOCUMENT_AUTH_OR_NETWORK_FAILED")
	}
	if resp == nil || resp.ApiResp == nil || resp.StatusCode != 200 || !resp.Success() || resp.Data == nil || resp.Data.Content == nil {
		return "", errors.New("DOCUMENT_PERMISSION_OR_CONTENT_FAILED")
	}
	var wire struct {
		Code *int `json:"code"`
	}
	if err = json.Unmarshal(resp.RawBody, &wire); err != nil || wire.Code == nil || *wire.Code != 0 {
		return "", errors.New("DOCUMENT_PERMISSION_OR_CONTENT_FAILED")
	}
	text := strings.TrimSpace(*resp.Data.Content)
	if text == "" || !utf8.ValidString(text) {
		return "", errors.New("DOCUMENT_EMPTY")
	}
	if len(text) > 12000 {
		return "", errors.New("DOCUMENT_TOO_LARGE")
	}
	return text, nil
}
