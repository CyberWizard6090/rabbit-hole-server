package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mailpitAPI(t *testing.T) string {
	t.Helper()
	api := os.Getenv("MAILPIT_API")
	if api == "" {
		t.Skip("MAILPIT_API is not set; start Mailpit (docker compose --profile mail up) and set MAILPIT_API=http://127.0.0.1:8025")
	}
	return strings.TrimRight(api, "/")
}

func mailpitDo(t *testing.T, method, url string, into any) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %d", method, url, resp.StatusCode)
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMailpitReceivesMessagesFromTheSMTPSender(t *testing.T) {
	api := mailpitAPI(t)
	port := 1025
	if v := os.Getenv("MAILPIT_SMTP_PORT"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		port = parsed
	}
	mailpitDo(t, http.MethodDelete, api+"/api/v1/messages", nil)

	sender, err := NewSMTP(SMTPOptions{
		Host: "127.0.0.1", Port: port, Security: SecurityNone, From: "Rabbit Hole <no-reply@rabbit-hole.test>",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = sender.Send(ctx, Message{
		To:      "Анна <anna@example.test>",
		Subject: "Сброс пароля",
		Text:    "Откройте ссылку: http://localhost:3000/reset-password?token=AbC-123_x=y\nСсылка действует 1 ч.",
		HTML:    `<p><a href="http://localhost:3000/reset-password?token=AbC-123_x=y">Сбросить пароль</a></p>`,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	var list struct {
		Total    int `json:"total"`
		Messages []struct {
			ID      string `json:"ID"`
			Subject string `json:"Subject"`
			From    struct{ Name, Address string }
			To      []struct{ Name, Address string }
		} `json:"messages"`
	}
	mailpitDo(t, http.MethodGet, api+"/api/v1/messages", &list)
	if list.Total != 1 || len(list.Messages) != 1 {
		t.Fatalf("Mailpit holds %d messages, want 1", list.Total)
	}
	got := list.Messages[0]
	if got.Subject != "Сброс пароля" || got.From.Address != "no-reply@rabbit-hole.test" || got.From.Name != "Rabbit Hole" ||
		len(got.To) != 1 || got.To[0].Address != "anna@example.test" || got.To[0].Name != "Анна" {
		t.Fatalf("message = %+v", got)
	}

	var full struct {
		Text string
		HTML string
	}
	mailpitDo(t, http.MethodGet, api+"/api/v1/message/"+got.ID, &full)
	if !strings.Contains(full.Text, "token=AbC-123_x=y") || !strings.Contains(full.Text, "Ссылка действует 1 ч.") {
		t.Fatalf("text part = %q", full.Text)
	}
	if !strings.Contains(full.HTML, `href="http://localhost:3000/reset-password?token=AbC-123_x=y"`) || !strings.Contains(full.HTML, "Сбросить пароль") {
		t.Fatalf("html part = %q", full.HTML)
	}
	mailpitDo(t, http.MethodDelete, api+"/api/v1/messages", nil)
}
