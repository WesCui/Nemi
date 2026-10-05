package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"nemi/internal/webreader"
)

// Service credentials never enter model input, tool arguments, or API replies.
type ServiceCredential struct {
	Key   string `json:"api_key,omitempty"`
	Email string `json:"email,omitempty"`
	Code  string `json:"authorization_code,omitempty"`
}

func ServiceID(id string) bool { return id == "search" || id == "amap" || id == "mail" }
func (c ServiceCredential) Valid(id string) bool {
	if id == "search" || id == "amap" {
		return len(c.Key) >= 16 && len(c.Key) <= 512 && !strings.ContainsAny(c.Key, " \r\n\t") && c.Email == "" && c.Code == ""
	}
	return id == "mail" && c.Key == "" && MailHost(c.Email) != "" && len(c.Code) >= 8 && len(c.Code) <= 128 && !strings.ContainsAny(c.Code, " \r\n\t")
}
func MailHost(email string) string {
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > 120 {
		return ""
	}
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return ""
	}
	switch strings.ToLower(domain) {
	case "qq.com", "foxmail.com":
		return "imap.qq.com"
	case "163.com":
		return "imap.163.com"
	case "126.com":
		return "imap.126.com"
	}
	return ""
}

type Online struct{ Client *http.Client }

func NewOnline() *Online {
	c := webreader.New().Client()
	// Credential-bearing requests must never follow redirects to another origin.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Online{Client: c}
}
func (o *Online) request(ctx context.Context, method, target, key string, body any, result any) error {
	var input io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		input = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, input)
	if err != nil {
		return errors.New("SERVICE_REQUEST_FAILED")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	r, err := o.Client.Do(req)
	if err != nil {
		return errors.New("SERVICE_REQUEST_FAILED")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return errors.New("SERVICE_REQUEST_FAILED")
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 || json.Unmarshal(b, result) != nil {
		return errors.New("SERVICE_RESPONSE_INVALID")
	}
	return nil
}
func clipped(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}

type SearchHit struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Summary   string `json:"summary"`
	Published string `json:"published,omitempty"`
}
type SearchResult struct {
	Provider  string      `json:"provider"`
	Query     string      `json:"query"`
	FetchedAt time.Time   `json:"fetched_at"`
	Results   []SearchHit `json:"results"`
}

func (o *Online) Search(ctx context.Context, c ServiceCredential, query, freshness string) (SearchResult, error) {
	if !c.Valid("search") {
		return SearchResult{}, errors.New("APP_NOT_CONNECTED")
	}
	if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 200 {
		return SearchResult{}, errors.New("INVALID_ARGUMENTS")
	}
	switch freshness {
	case "", "noLimit":
		freshness = "noLimit"
	case "oneDay", "oneWeek", "oneMonth", "oneYear":
	default:
		return SearchResult{}, errors.New("INVALID_ARGUMENTS")
	}
	var raw struct {
		Code int `json:"code"`
		Data struct {
			WebPages struct {
				Value []struct{ Name, URL, Snippet, Summary, DatePublished string } `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if err := o.request(ctx, "POST", "https://api.bochaai.com/v1/web-search", c.Key, map[string]any{"query": query, "freshness": freshness, "summary": true, "count": 5}, &raw); err != nil {
		return SearchResult{}, err
	}
	if raw.Code != 200 {
		return SearchResult{}, errors.New("SERVICE_REQUEST_FAILED")
	}
	out := SearchResult{Provider: "博查", Query: query, FetchedAt: time.Now().UTC(), Results: []SearchHit{}}
	for _, p := range raw.Data.WebPages.Value {
		if webreader.CheckURL(p.URL) != nil || len(p.URL) > 1000 {
			continue
		}
		summary := p.Summary
		if summary == "" {
			summary = p.Snippet
		}
		out.Results = append(out.Results, SearchHit{clipped(p.Name, 100), p.URL, clipped(summary, 300), clipped(p.DatePublished, 80)})
		if len(out.Results) >= 5 {
			break
		}
	}
	return out, nil
}

type Place struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	City     string `json:"city"`
	Location string `json:"location"`
}

func (o *Online) Places(ctx context.Context, c ServiceCredential, query, city string) (any, error) {
	if !c.Valid("amap") {
		return nil, errors.New("APP_NOT_CONNECTED")
	}
	if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 80 || strings.TrimSpace(city) == "" || utf8.RuneCountInString(city) > 40 {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	q := url.Values{"key": {c.Key}, "keywords": {query}, "region": {city}, "city_limit": {"true"}, "page_size": {"5"}, "page_num": {"1"}}
	var raw struct {
		Status string
		POIs   []struct {
			ID, Name, Location, CityName string
			Address                      any
		} `json:"pois"`
	}
	if err := o.request(ctx, "GET", "https://restapi.amap.com/v5/place/text?"+q.Encode(), "", nil, &raw); err != nil {
		return nil, err
	}
	if raw.Status != "1" {
		return nil, errors.New("SERVICE_REQUEST_FAILED")
	}
	places := []Place{}
	for _, p := range raw.POIs {
		address, _ := p.Address.(string)
		if !ValidCoordinate(p.Location) {
			continue
		}
		places = append(places, Place{clipped(p.ID, 80), clipped(p.Name, 100), clipped(address, 200), clipped(p.CityName, 40), p.Location})
		if len(places) == 5 {
			break
		}
	}
	return map[string]any{"provider": "高德地图", "fetched_at": time.Now().UTC(), "coordinate_system": "GCJ-02", "places": places}, nil
}

var coordinate = regexp.MustCompile(`^-?\d{1,3}(\.\d{1,8})?,-?\d{1,2}(\.\d{1,8})?$`)

func ValidCoordinate(s string) bool {
	if !coordinate.MatchString(s) {
		return false
	}
	var x, y float64
	parts := strings.Split(s, ",")
	if json.Unmarshal([]byte(parts[0]), &x) != nil || json.Unmarshal([]byte(parts[1]), &y) != nil {
		return false
	}
	return x >= -180 && x <= 180 && y >= -90 && y <= 90
}

type RouteStep struct {
	Instruction string `json:"instruction"`
	Distance    string `json:"distance_m"`
	Duration    string `json:"duration_seconds"`
}

func (o *Online) Route(ctx context.Context, c ServiceCredential, origin, destination, mode string) (any, error) {
	if !c.Valid("amap") {
		return nil, errors.New("APP_NOT_CONNECTED")
	}
	if !ValidCoordinate(origin) || !ValidCoordinate(destination) || (mode != "walking" && mode != "driving") {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	q := url.Values{"key": {c.Key}, "origin": {origin}, "destination": {destination}, "extensions": {"base"}, "output": {"json"}}
	var raw struct {
		Status string
		Route  struct {
			Paths []struct {
				Distance, Duration string
				Steps              []struct{ Instruction, Distance, Duration string }
			}
		}
	}
	if err := o.request(ctx, "GET", "https://restapi.amap.com/v3/direction/"+mode+"?"+q.Encode(), "", nil, &raw); err != nil {
		return nil, err
	}
	if raw.Status != "1" {
		return nil, errors.New("SERVICE_REQUEST_FAILED")
	}
	paths := []map[string]any{}
	for _, p := range raw.Route.Paths {
		steps := []RouteStep{}
		for _, s := range p.Steps {
			steps = append(steps, RouteStep{clipped(s.Instruction, 100), clipped(s.Distance, 20), clipped(s.Duration, 20)})
			if len(steps) == 12 {
				break
			}
		}
		paths = append(paths, map[string]any{"distance_m": clipped(p.Distance, 20), "duration_seconds": clipped(p.Duration, 20), "steps": steps, "total_steps": len(p.Steps)})
		if len(paths) == 2 {
			break
		}
	}
	return map[string]any{"provider": "高德地图", "fetched_at": time.Now().UTC(), "mode": mode, "origin": origin, "destination": destination, "coordinate_system": "GCJ-02", "paths": paths, "note": "按查询时刻估算，不保证未来交通状况"}, nil
}
