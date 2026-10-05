package connectors

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

// Exercise the actual IMAP parser and wire commands, using no real mailbox.
func TestMailExaminePeekAndVersionFence(t *testing.T) {
	operations := make(chan string, 30)
	m := &Mail{Dial: func(_ context.Context, host string) (net.Conn, error) {
		if host != "imap.qq.com" {
			t.Error("mail host changed")
		}
		left, right := net.Pipe()
		go func() {
			defer right.Close()
			fmt.Fprint(right, "* OK [CAPABILITY IMAP4rev1] fixture\r\n")
			reader := bufio.NewReader(right)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
				if len(parts) != 2 {
					return
				}
				tag, command := parts[0], parts[1]
				switch {
				case strings.HasPrefix(command, "LOGIN "):
					operations <- "LOGIN"
				case command == "EXAMINE INBOX":
					operations <- "EXAMINE"
					fmt.Fprint(right, "* 1 EXISTS\r\n* FLAGS (\\Seen)\r\n* OK [UIDVALIDITY 777] fixed\r\n")
				case strings.HasPrefix(command, "FETCH "):
					operations <- "FETCH"
					fmt.Fprint(right, "* 1 FETCH (UID 42 FLAGS () ENVELOPE (\"Mon, 5 Oct 2026 10:00:00 +0800\" \"=?UTF-8?B?5Lya6K6u5a6J5o6S?=\" ((NIL NIL \"sender\" \"example.com\")) NIL NIL NIL NIL NIL NIL NIL))\r\n")
				case strings.HasPrefix(command, "UID FETCH "):
					if !strings.Contains(command, "BODY.PEEK[]<0.262145>") {
						operations <- "UNSAFE_BODY"
					} else {
						operations <- "PEEK"
					}
					raw := "Subject: meeting\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n明天下午开会，请准备材料。"
					fmt.Fprintf(right, "* 1 FETCH (UID 42 FLAGS () RFC822.SIZE %d BODY[]<0> {%d}\r\n%s)\r\n", len(raw), len(raw), raw)
				default:
					operations <- "UNEXPECTED"
				}
				fmt.Fprintf(right, "%s OK done\r\n", tag)
			}
		}()
		return left, nil
	}}
	c := ServiceCredential{Email: "fixture@qq.com", Code: "fixture-authorization-code"}
	listed, err := m.List(context.Background(), c, 3, 0)
	if err != nil {
		t.Fatal("IMAP list failed")
	}
	list := listed.(map[string]any)["messages"].([]MailHeader)
	if len(list) != 1 || list[0].ID != "3:777:42" || !list[0].Unread {
		t.Fatal("mail ID or unread state incorrect")
	}
	read, err := m.Read(context.Background(), c, 3, list[0].ID)
	if err != nil || !strings.Contains(read.(map[string]any)["text"].(string), "准备材料") {
		t.Fatal("IMAP body failed")
	}
	if _, err = m.Read(context.Background(), c, 4, list[0].ID); err == nil || err.Error() != "MAIL_STALE_ID" {
		t.Fatal("stale connection read another mailbox")
	}
	close(operations)
	seen := false
	for op := range operations {
		if op == "PEEK" {
			seen = true
		}
		if op == "UNSAFE_BODY" || op == "UNEXPECTED" {
			t.Fatal("mail operation was not read-only")
		}
	}
	if !seen {
		t.Fatal("body was not read with PEEK")
	}
}
func TestMailMIMETextIsStaticAndIgnoresAttachment(t *testing.T) {
	raw := `MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=bound

--bound
Content-Type: text/html; charset=UTF-8

<div>邮件正文</div><script>fetch('https://evil.example')</script>
--bound
Content-Type: text/plain
Content-Disposition: attachment; filename=private.txt

不应读取附件
--bound--
`
	text, err := mailText([]byte(strings.ReplaceAll(raw, "\n", "\r\n")))
	if err != nil || text != "邮件正文" {
		t.Fatal("attachment or script entered mail text")
	}
}
