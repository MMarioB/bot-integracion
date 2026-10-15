package fetch

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mario/ofertas-bot/internal/job"
)

// RSS lee cualquier feed RSS 2.0. Sirve para el BOCM
// (https://www.bocm.es/ultimo-boletin.xml) y para cualquier portal que publique feed.
// No filtra: de eso se encarga el paquete filter.
type RSS struct {
	SourceName string
	URL        string
	Location   string // ubicación por defecto si el feed no la trae (p. ej. "Comunidad de Madrid")
	Label      string // nombre a mostrar como entidad; vacío = título del canal RSS
	// TitleFromDescription usa la descripción como título. Para feeds como el del BOCM,
	// donde el título es genérico ("Orden Nº 30 / Boletín Nº 246") y el contenido va en la descripción.
	TitleFromDescription bool
	HTTP                 *http.Client
}

func (f *RSS) Name() string { return f.SourceName }

type rssDoc struct {
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func (f *RSS) Fetch(ctx context.Context) ([]job.Job, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ofertas-bot/0.1")
	c := f.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.SourceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", f.SourceName, resp.StatusCode)
	}

	var doc rssDoc
	dec := xml.NewDecoder(resp.Body)
	dec.CharsetReader = identityCharset // algunos boletines declaran ISO-8859-1
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", f.SourceName, err)
	}

	jobs := make([]job.Job, 0, len(doc.Channel.Items))
	for _, it := range doc.Channel.Items {
		id := strings.TrimSpace(it.GUID)
		if id == "" {
			id = strings.TrimSpace(it.Link)
		}
		published, _ := time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate))
		title, summary := clean(it.Title), clean(it.Description)
		if f.TitleFromDescription && summary != "" {
			title, summary = summary, title
		}
		company := f.Label
		if company == "" {
			company = clean(doc.Channel.Title)
		}
		jobs = append(jobs, job.Job{
			ID:        id,
			Source:    f.SourceName,
			Title:     title,
			Company:   company,
			Location:  f.Location,
			URL:       strings.TrimSpace(it.Link),
			Summary:   summary,
			Published: published,
		})
	}
	return jobs, nil
}

var tagRe = regexp.MustCompile(`<[^>]*>`)
var spaceRe = regexp.MustCompile(`\s+`)

// clean quita HTML, entidades y espacios sobrantes.
func clean(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}
