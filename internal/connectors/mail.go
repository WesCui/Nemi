package connectors

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	imapid "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"
	"nemi/internal/webreader"
)

const maxMailBytes = 256 << 10

type Mail struct {
	Dial func(context.Context, string) (net.Conn, error)
}

func NewMail() *Mail {
	return &Mail{Dial: func(ctx context.Context, host string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		conn, err := webreader.DialPublic(ctx, host, "993")
		if err != nil {
			return nil, err
		}
		secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err = secure.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return secure, nil
	}}
}
func (m *Mail) open(ctx context.Context, c ServiceCredential) (*client.Client, *imap.MailboxStatus, func(), error) {
	if !c.Valid("mail") {
		return nil, nil, nil, errors.New("APP_NOT_CONNECTED")
	}
	conn, err := m.Dial(ctx, MailHost(c.Email))
	if err != nil {
		return nil, nil, nil, errors.New("MAIL_AUTH_OR_NETWORK_FAILED")
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	cleanup := func() { stop(); conn.Close() }
	cl, err := client.New(conn)
	if err != nil {
		cleanup()
		return nil, nil, nil, errors.New("MAIL_AUTH_OR_NETWORK_FAILED")
	}
	cl.Timeout = 12 * time.Second
	id := imapid.NewClient(cl)
	if supported, e := id.SupportID(); e == nil && supported {
		_, err = id.ID(imapid.ID{"name": "Nemi", "version": "0.12.0"})
		if err != nil {
			cleanup()
			return nil, nil, nil, errors.New("MAIL_AUTH_OR_NETWORK_FAILED")
		}
	}
	if err = cl.Login(c.Email, c.Code); err != nil {
		cleanup()
		return nil, nil, nil, errors.New("MAIL_AUTH_OR_NETWORK_FAILED")
	}
	box, err := cl.Select("INBOX", true) // EXAMINE, never SELECT/read-write.
	if err != nil {
		cleanup()
		return nil, nil, nil, errors.New("MAIL_AUTH_OR_NETWORK_FAILED")
	}
	return cl, box, cleanup, nil
}

type MailHeader struct {
	ID      string    `json:"mail_id"`
	Subject string    `json:"subject"`
	From    string    `json:"from"`
	Date    time.Time `json:"date"`
	Unread  bool      `json:"unread"`
}

func mailHeader(m *imap.Message, revision int, validity uint32) MailHeader {
	h := MailHeader{ID: fmt.Sprintf("%d:%d:%d", revision, validity, m.Uid), Unread: true}
	if m.Envelope != nil {
		h.Subject = clipped(m.Envelope.Subject, 180)
		h.Date = m.Envelope.Date
		from := []string{}
		for _, a := range m.Envelope.From {
			from = append(from, a.Address())
			if len(from) == 3 {
				break
			}
		}
		h.From = clipped(strings.Join(from, ", "), 200)
	}
	for _, flag := range m.Flags {
		if flag == imap.SeenFlag {
			h.Unread = false
		}
	}
	return h
}
func (m *Mail) List(ctx context.Context, c ServiceCredential, revision, offset int) (any, error) {
	if offset < 0 || offset > 1000 {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	cl, box, cleanup, err := m.open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	list := []MailHeader{}
	total := int(box.Messages)
	if offset >= total {
		return map[string]any{"messages": list, "total": total, "next_offset": total, "has_more": false}, nil
	}
	end := total - offset
	start := max(1, end-9)
	seq := new(imap.SeqSet)
	seq.AddRange(uint32(start), uint32(end))
	ch := make(chan *imap.Message, 10)
	if err = cl.Fetch(seq, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags}, ch); err != nil {
		return nil, errors.New("MAIL_READ_FAILED")
	}
	for msg := range ch {
		list = append(list, mailHeader(msg, revision, box.UidValidity))
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	next := offset + (end - start + 1)
	return map[string]any{"messages": list, "total": total, "next_offset": next, "has_more": next < total, "mailbox": "INBOX", "note": "按收件箱序号倒序分页；邮箱发生变化时页面可能移动，mail_id 仍按 UID 定位"}, nil
}
func (m *Mail) Read(ctx context.Context, c ServiceCredential, revision int, id string) (any, error) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	r, err := strconv.Atoi(parts[0])
	validity, e := strconv.ParseUint(parts[1], 10, 32)
	uid, e2 := strconv.ParseUint(parts[2], 10, 32)
	if err != nil || e != nil || e2 != nil || r != revision || validity == 0 || uid == 0 {
		return nil, errors.New("MAIL_STALE_ID")
	}
	cl, box, cleanup, err := m.open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if box.UidValidity != uint32(validity) {
		return nil, errors.New("MAIL_STALE_ID")
	}
	seq := new(imap.SeqSet)
	seq.AddNum(uint32(uid))
	section := &imap.BodySectionName{Peek: true, Partial: []int{0, maxMailBytes + 1}}
	ch := make(chan *imap.Message, 1)
	if cl.UidFetch(seq, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags, imap.FetchRFC822Size, section.FetchItem()}, ch) != nil {
		return nil, errors.New("MAIL_READ_FAILED")
	}
	msg := <-ch
	if msg == nil {
		return nil, errors.New("MAIL_NOT_FOUND")
	}
	if msg.Size > maxMailBytes {
		return nil, errors.New("MAIL_TOO_LARGE")
	}
	body := msg.GetBody(section)
	if body == nil {
		return nil, errors.New("MAIL_READ_FAILED")
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxMailBytes+1))
	if err != nil || len(raw) > maxMailBytes {
		return nil, errors.New("MAIL_TOO_LARGE")
	}
	text, err := mailText(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": mailHeader(msg, revision, box.UidValidity), "text": clipped(text, 3000), "truncated": len([]rune(text)) > 3000, "attachments_read": false, "readonly": true}, nil
}
func mailText(raw []byte) (string, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("MAIL_READ_FAILED")
	}
	defer r.Close()
	plain, rich := "", ""
	for i := 0; i < 30; i++ {
		p, e := r.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", errors.New("MAIL_READ_FAILED")
		}
		h, ok := p.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		kind, _, _ := h.ContentType()
		if kind != "text/plain" && kind != "text/html" {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(p.Body, maxMailBytes+1))
		if e != nil || len(b) > maxMailBytes {
			return "", errors.New("MAIL_TOO_LARGE")
		}
		if kind == "text/plain" {
			plain += string(b) + "\n"
		} else {
			rich += staticHTML(string(b)) + "\n"
		}
		if len(plain)+len(rich) > maxMailBytes {
			return "", errors.New("MAIL_TOO_LARGE")
		}
	}
	if strings.TrimSpace(plain) != "" {
		return strings.TrimSpace(plain), nil
	}
	return strings.TrimSpace(rich), nil
}
func staticHTML(raw string) string {
	z := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	skip := 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		t := z.Token()
		switch kind {
		case html.StartTagToken:
			if t.Data == "script" || t.Data == "style" {
				skip++
			}
			if t.Data == "p" || t.Data == "br" || t.Data == "div" {
				out.WriteByte('\n')
			}
		case html.EndTagToken:
			if (t.Data == "script" || t.Data == "style") && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				out.WriteString(t.Data)
			}
		}
	}
	return strings.TrimSpace(out.String())
}
