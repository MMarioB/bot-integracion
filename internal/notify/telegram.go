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

// SourceLabel da un nombre legible a cada fuente.
func SourceLabel(src string) string {
	switch src {
	case "hacesfalta":
		return "Hacesfalta.org"
	case "infojobs":
		return "InfoJobs"
	case "adzuna":
		return "Adzuna"
	case "bocm":
		return "el BOCM"
	case "boe":
		return "el BOE"
	case "boam":
		return "el BOAM"
	}
	return src
}

// isPublic indica si la fuente es un boletín oficial (convocatorias públicas).
func isPublic(src string) bool {
	return src == "boe" || src == "bocm" || src == "boam"
}

// FormatJob compone el mensaje HTML de una oferta.
func FormatJob(j job.Job) string {
	var b strings.Builder
	b.WriteString("✨ <i>Ha salido algo que puede encajarte</i>\n\n")
	fmt.Fprintf(&b, "<b>%s</b>\n", html.EscapeString(truncate(j.Title, 250)))
	if j.Company != "" {
		fmt.Fprintf(&b, "🏢 %s\n", html.EscapeString(j.Company))
	}
	if j.Location != "" {
		fmt.Fprintf(&b, "📍 %s\n", html.EscapeString(j.Location))
	}
	if j.Summary != "" {
		fmt.Fprintf(&b, "📝 %s\n", html.EscapeString(truncate(j.Summary, 300)))
	}
	if isPublic(j.Source) {
		b.WriteString("ℹ️ Es una convocatoria pública: en el enlace están las bases y el plazo para presentarse.\n")
	}
	fmt.Fprintf(&b, "\n👉 <a href=\"%s\">Ver la oferta</a> <i>(vía %s)</i>",
		html.EscapeString(j.URL), html.EscapeString(SourceLabel(j.Source)))
	return b.String()
}

// maxMsg deja margen bajo el límite de 4096 caracteres de Telegram.
const maxMsg = 3800

const intro = "👋 <b>¡Hola! Soy tu buscador de ofertas de integración social en Madrid.</b>\n\n" +
	"Cada pocas horas reviso por ti portales de empleo y boletines oficiales, " +
	"para que no tengas que estar mirando tú. Cuando salga algo que encaje, te llega aquí.\n\n"

const calmFooter = "\n💛 Sin prisa: no hace falta mirarlas todas hoy. Yo sigo buscando."

// FormatDigest compone el mensaje de bienvenida con las ofertas abiertas ahora mismo,
// partido en varios mensajes si no cabe en uno.
func FormatDigest(jobs []job.Job) []string {
	if len(jobs) == 0 {
		return []string{intro + "Ahora mismo no hay nada abierto que encaje, pero esto se mueve: " +
			"en cuanto aparezca algo, te escribo. 💛"}
	}
	header := intro + fmt.Sprintf("Para empezar, estas son las <b>%d ofertas abiertas</b> ahora mismo que encajan contigo:\n\n", len(jobs))
	return digest(header, jobs, calmFooter)
}

// FormatSourceDigest presenta de golpe lo que ya había en una fuente recién añadida,
// en vez de mandar cada oferta como si fuera nueva.
func FormatSourceDigest(source string, jobs []job.Job) []string {
	header := fmt.Sprintf("🔎 <b>Ahora también busco en %s</b>\nHe encontrado <b>%d ofertas abiertas</b> que encajan contigo:\n\n",
		html.EscapeString(source), len(jobs))
	return digest(header, jobs, calmFooter)
}

func digest(header string, jobs []job.Job, footer string) []string {
	var msgs []string
	var b strings.Builder
	b.WriteString(header)
	for i, j := range jobs {
		var line strings.Builder
		fmt.Fprintf(&line, "%d. <a href=\"%s\">%s</a>", i+1, html.EscapeString(j.URL), html.EscapeString(truncate(j.Title, 200)))
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
	if b.Len()+len(footer) > maxMsg {
		msgs = append(msgs, b.String())
		b.Reset()
	}
	b.WriteString(footer)
	msgs = append(msgs, b.String())
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
