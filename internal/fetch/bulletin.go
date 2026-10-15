package fetch

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/mario/ofertas-bot/internal/job"
)

// Bulletin es para boletines cuyo RSS solo anuncia "ha salido el boletín de hoy"
// (como el BOAM del Ayuntamiento de Madrid). Lee el RSS, entra en la página de los
// boletines más recientes y convierte cada anuncio del sumario en un Job.
type Bulletin struct {
	SourceName   string
	FeedURL      string
	Label        string // p. ej. "BOAM"
	Location     string
	MaxBulletins int // cuántos boletines recientes revisar en cada pasada (por defecto 2)
	HTTP         *http.Client
}

func (f *Bulletin) Name() string { return f.SourceName }

func (f *Bulletin) Fetch(ctx context.Context) ([]job.Job, error) {
	feed := &RSS{SourceName: f.SourceName, URL: f.FeedURL, HTTP: f.HTTP}
	items, err := feed.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, k int) bool { return items[i].Published.After(items[k].Published) })
	max := f.MaxBulletins
	if max <= 0 {
		max = 2
	}
	if len(items) > max {
		items = items[:max]
	}

	var out []job.Job
	for _, b := range items {
		page, err := getHTML(ctx, f.HTTP, b.URL)
		if err != nil {
			return nil, fmt.Errorf("%s: boletín %q: %w", f.SourceName, b.Title, err)
		}
		name := bulletinName(b.Title)
		company := f.Label
		if name != "" {
			company = strings.TrimSpace(f.Label + " · " + name)
		}
		for _, a := range parseAnnouncements(page, b.URL) {
			out = append(out, job.Job{
				ID:        a.href,
				Source:    f.SourceName,
				Title:     a.title,
				Company:   company,
				Location:  f.Location,
				URL:       a.href,
				Published: b.Published,
			})
		}
	}
	return out, nil
}

// "10234,BOAM de 15 de octubre de 2026" -> "nº 10234 (15 de octubre de 2026)"
var bulletinTitleRe = regexp.MustCompile(`^\s*(\d+)\s*,\s*\S+\s+de\s+(.+?)\s*$`)

func bulletinName(t string) string {
	if m := bulletinTitleRe.FindStringSubmatch(t); m != nil {
		return "nº " + m[1] + " (" + m[2] + ")"
	}
	return t
}

type announcement struct{ title, href string }

var anchorRe = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
var dropBlocksRe = regexp.MustCompile(`(?is)<(script|style|noscript|head)\b.*?</(script|style|noscript|head)>`)

const (
	minTitle    = 30  // por debajo, el texto del enlace es tipo "Descargar PDF"
	maxContext  = 400 // caracteres de texto previo que se usan como título
	maxAnnounce = 500 // tope por boletín
)

// parseAnnouncements saca los anuncios de la página de un boletín sin depender de su
// maquetación: cada enlace con su texto, o con el texto que lo precede si el del
// enlace es corto. El ruido (menús, pie) lo descarta luego el filtro de palabras clave.
func parseAnnouncements(page, pageURL string) []announcement {
	page = dropBlocksRe.ReplaceAllString(page, " ")
	base, _ := url.Parse(pageURL)

	seen := map[string]bool{}
	var out []announcement
	prevEnd := 0
	for _, m := range anchorRe.FindAllStringSubmatchIndex(page, -1) {
		rawHref, text := page[m[2]:m[3]], clean(page[m[4]:m[5]])
		before := clean(page[prevEnd:m[0]])
		prevEnd = m[1]

		href := resolve(base, rawHref)
		if href == "" || seen[href] {
			continue
		}

		title := text
		if len([]rune(title)) < minTitle {
			title = tail(before, maxContext)
		}
		if len([]rune(title)) < minTitle {
			continue
		}
		seen[href] = true
		out = append(out, announcement{title: title, href: href})
		if len(out) >= maxAnnounce {
			break
		}
	}
	return out
}

func resolve(base *url.URL, raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") ||
		strings.HasPrefix(strings.ToLower(raw), "mailto:") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	return u.String()
}

// tail devuelve los últimos n caracteres, cortando por un espacio para no partir palabras.
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	t := string(r[len(r)-n:])
	if i := strings.IndexByte(t, ' '); i >= 0 {
		t = t[i+1:]
	}
	return "…" + t
}
