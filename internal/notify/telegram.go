// Package notify envía las alertas.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mario/ofertas-bot/internal/job"
)

type Notifier interface {
	Job(ctx context.Context, j job.Job) error
	Text(ctx context.Context, msg string) error
}

// Telegram envía mensajes con la Bot API (token de @BotFather).
type Telegram struct {
	Token   string
	ChatID  string
	BaseURL string // vacío = https://api.telegram.org
	HTTP    *http.Client
}

func (t *Telegram) Job(ctx context.Context, j job.Job) error {
	return t.send(ctx, FormatJob(j))
}

func (t *Telegram) Text(ctx context.Context, msg string) error {
	return t.send(ctx, html.EscapeString(msg))
}

func (t *Telegram) send(ctx context.Context, text string) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	payload, _ := json.Marshal(map[string]any{
		"chat_id":                  t.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", base, t.Token), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := t.HTTP
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, body)
	}
	// Telegram limita ~1 msg/s por chat; evitamos el 429 en ráfagas.
	time.Sleep(1100 * time.Millisecond)
	return nil
}

// FormatJob compone el mensaje HTML de una oferta.
func FormatJob(j job.Job) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🆕 <b>%s</b>\n", html.EscapeString(j.Title))
	if j.Company != "" {
		fmt.Fprintf(&b, "🏢 %s\n", html.EscapeString(j.Company))
	}
	if j.Location != "" {
		fmt.Fprintf(&b, "📍 %s\n", html.EscapeString(j.Location))
	}
	if j.Summary != "" {
		fmt.Fprintf(&b, "📝 %s\n", html.EscapeString(truncate(j.Summary, 300)))
	}
	fmt.Fprintf(&b, "🔗 <a href=\"%s\">Ver oferta</a> · <i>%s</i>", html.EscapeString(j.URL), html.EscapeString(j.Source))
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Stdout imprime en consola; útil con -dry-run.
type Stdout struct{}

func (Stdout) Job(_ context.Context, j job.Job) error {
	fmt.Println(FormatJob(j))
	fmt.Println("---")
	return nil
}

func (Stdout) Text(_ context.Context, msg string) error {
	fmt.Println(msg)
	return nil
}
