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
	Text(ctx context.Context, msg string) error // texto plano (se escapa)
	HTML(ctx context.Context, msg string) error // HTML ya formateado
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

func (t *Telegram) HTML(ctx context.Context, msg string) error {
	return t.send(ctx, msg)
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

// maxMsg deja margen bajo el límite de 4096 caracteres de Telegram.
const maxMsg = 3800

// FormatDigest compone el resumen inicial con todas las ofertas actuales,
// partido en varios mensajes si no cabe en uno.
func FormatDigest(jobs []job.Job) []string {
	if len(jobs) == 0 {
		return []string{"👋 <b>Bot de ofertas en marcha.</b>\nAhora mismo no hay ofertas publicadas que encajen. " +
			"Te aviso en cuanto salga alguna."}
	}
	header := fmt.Sprintf("👋 <b>Bot de ofertas en marcha.</b>\nEstas son las %d ofertas publicadas ahora mismo. "+
		"A partir de aquí te aviso solo de las nuevas.\n\n", len(jobs))

	var msgs []string
	var b strings.Builder
	b.WriteString(header)
	for i, j := range jobs {
		var line strings.Builder
		fmt.Fprintf(&line, "%d. <a href=\"%s\">%s</a>", i+1, html.EscapeString(j.URL), html.EscapeString(truncate(j.Title, 120)))
		var meta []string
		if j.Company != "" {
			meta = append(meta, j.Company)
		}
		if j.Location != "" {
			meta = append(meta, j.Location)
		}
		if len(meta) > 0 {
			fmt.Fprintf(&line, "\n    <i>%s</i>", html.EscapeString(truncate(strings.Join(meta, " · "), 120)))
		}
		line.WriteString("\n")

		if b.Len()+line.Len() > maxMsg {
			msgs = append(msgs, b.String())
			b.Reset()
		}
		b.WriteString(line.String())
	}
	if b.Len() > 0 {
		msgs = append(msgs, b.String())
	}
	return msgs
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

func (Stdout) HTML(_ context.Context, msg string) error {
	fmt.Println(msg)
	return nil
}
