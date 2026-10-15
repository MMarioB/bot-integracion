package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/mario/ofertas-bot/internal/filter"
	"github.com/mario/ofertas-bot/internal/job"
)

// Hacesfalta busca en las ofertas remuneradas de hacesfalta.org (portal de empleo del tercer sector).
// No tiene RSS ni API: usa el buscador por URL y extrae las ofertas del HTML del listado.
// robots.txt permite estas páginas; solo hacemos una petición por búsqueda.
type Hacesfalta struct {
	Queries  []string // términos para el buscador ("integrador social"...)
	Province string   // solo se quedan ofertas cuya ubicación contenga esto ("Madrid"); vacío = todas
	BaseURL  string   // vacío = https://www.hacesfalta.org
	HTTP     *http.Client
}

func (f *Hacesfalta) Name() string { return "hacesfalta" }

// Enlace a la ficha de una oferta: /oportunidades/remuneradas/detalle/<slug>/<id>
var hfLinkRe = regexp.MustCompile(`(?is)<a\b[^>]*href="([^"]*/oportunidades/remuneradas/detalle/[^"/]+/(\d+))[^"]*"[^>]*>(.*?)</a>`)

// La ubicación aparece como "España(Madrid)" o "España (Madrid)".
var hfLocRe = regexp.MustCompile(`(?i)Espa(?:ñ|&ntilde;|n)a\s*\(\s*([^()]+?)\s*\)`)

func (f *Hacesfalta) Fetch(ctx context.Context) ([]job.Job, error) {
	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		base = "https://www.hacesfalta.org"
	}
	seen := map[string]bool{}
	var out []job.Job
	for _, q := range f.Queries {
		params := url.Values{}
		params.Set("clave", q)
		params.Set("fOrden", "FechaPubli")
		params.Set("fOrdenDir", "Desc")
		page := base + "/oportunidades/remuneradas/Default.aspx?" + params.Encode()

		body, err := getHTML(ctx, f.HTTP, page)
		if err != nil {
			return nil, fmt.Errorf("hacesfalta %q: %w", q, err)
		}
		for _, j := range parseHacesfalta(body, base) {
			if seen[j.ID] {
				continue
			}
			seen[j.ID] = true
			// Si no se pudo leer la ubicación, mejor avisar que perder la oferta.
			if f.Province != "" && j.Location != "" &&
				!strings.Contains(filter.Normalize(j.Location), filter.Normalize(f.Province)) {
				continue
			}
			out = append(out, j)
		}
	}
	return out, nil
}

// parseHacesfalta extrae las ofertas del HTML de un listado. Cada oferta es el tramo de
// HTML entre su primer enlace a la ficha y el primer enlace de la siguiente oferta.
func parseHacesfalta(page, base string) []job.Job {
	matches := hfLinkRe.FindAllStringSubmatchIndex(page, -1)

	type card struct {
		id, href, title string
		start           int
	}
	var cards []card
	byID := map[string]int{}
	for _, m := range matches {
		href, id, text := page[m[2]:m[3]], page[m[4]:m[5]], clean(page[m[6]:m[7]])
		if i, ok := byID[id]; ok {
			if cards[i].title == "" { // el primer enlace puede ser una imagen sin texto
				cards[i].title = text
			}
			continue
		}
		byID[id] = len(cards)
		cards = append(cards, card{id: id, href: href, title: text, start: m[0]})
	}

	jobs := make([]job.Job, 0, len(cards))
	for i, c := range cards {
		end := len(page)
		if i+1 < len(cards) {
			end = cards[i+1].start
		}
		text := clean(page[c.start:end])

		var loc, company string
		if lm := hfLocRe.FindStringSubmatchIndex(text); lm != nil {
			loc = text[lm[2]:lm[3]]
			// Entre el título y la ubicación suele ir la entidad.
			if t := strings.Index(text, c.title); c.title != "" && t >= 0 && t+len(c.title) <= lm[0] {
				company = strings.Trim(text[t+len(c.title):lm[0]], " |·•-–—,;:")
				if len([]rune(company)) > 80 {
					company = ""
				}
			}
		}

		link := c.href
		if strings.HasPrefix(link, "/") {
			link = base + link
		}
		title := c.title
		if title == "" {
			title = "Oferta en Hacesfalta.org"
		}
		jobs = append(jobs, job.Job{
			ID:       c.id,
			Source:   "hacesfalta",
			Title:    title,
			Company:  company,
			Location: loc,
			URL:      link,
		})
	}
	return jobs
}

func getHTML(ctx context.Context, c *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ofertas-bot/0.1 (alertas personales de empleo; 1 peticion cada 2h)")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	return string(b), err
}
