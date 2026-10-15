package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mario/ofertas-bot/internal/job"
)

// Adzuna consulta la API de Adzuna (https://developer.adzuna.com), un agregador que recoge
// ofertas de muchos portales y webs de empresas. Registro gratuito e inmediato:
// da un app_id y un app_key. Límite gratuito: 250 llamadas/día (el bot hace ~3 por pasada).
type Adzuna struct {
	AppID      string
	AppKey     string
	Country    string   // "es"
	Where      string   // "Madrid"
	Queries    []string // una llamada por término
	MaxDaysOld int      // solo ofertas de los últimos N días (por defecto 14)
	BaseURL    string   // vacío = https://api.adzuna.com
	HTTP       *http.Client
}

func (f *Adzuna) Name() string { return "adzuna" }

type adzunaResponse struct {
	Results []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Company struct {
			DisplayName string `json:"display_name"`
		} `json:"company"`
		Location struct {
			DisplayName string   `json:"display_name"`
			Area        []string `json:"area"`
		} `json:"location"`
		Description string `json:"description"`
		RedirectURL string `json:"redirect_url"`
		Created     string `json:"created"`
	} `json:"results"`
}

func (f *Adzuna) Fetch(ctx context.Context) ([]job.Job, error) {
	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		base = "https://api.adzuna.com"
	}
	country := f.Country
	if country == "" {
		country = "es"
	}
	days := f.MaxDaysOld
	if days <= 0 {
		days = 14
	}

	seen := map[string]bool{}
	var out []job.Job
	for _, q := range f.Queries {
		params := url.Values{}
		params.Set("app_id", f.AppID)
		params.Set("app_key", f.AppKey)
		params.Set("what_phrase", q) // frase exacta: evita "social media manager" y similares
		if f.Where != "" {
			params.Set("where", f.Where)
		}
		params.Set("results_per_page", "50")
		params.Set("sort_by", "date")
		params.Set("max_days_old", strconv.Itoa(days))
		params.Set("content-type", "application/json")

		u := fmt.Sprintf("%s/v1/api/jobs/%s/search/1?%s", base, url.PathEscape(country), params.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		var body adzunaResponse
		if err := doJSON(f.HTTP, req, &body); err != nil {
			// Los errores de red de net/http incluyen la URL, que lleva las claves:
			// nos quedamos solo con la causa para no filtrarlas a los logs.
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return nil, fmt.Errorf("adzuna %q: %w", q, err)
		}
		for _, r := range body.Results {
			if r.ID == "" || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			published, _ := time.Parse(time.RFC3339, r.Created)
			out = append(out, job.Job{
				ID:        r.ID,
				Source:    f.Name(),
				Title:     clean(r.Title), // Adzuna marca las coincidencias con <strong>
				Company:   clean(r.Company.DisplayName),
				Location:  adzunaLocation(r.Location.DisplayName, r.Location.Area),
				URL:       r.RedirectURL,
				Summary:   clean(r.Description),
				Published: published,
			})
		}
	}
	return out, nil
}

// adzunaLocation prefiere "Getafe, Comunidad de Madrid" a la jerarquía completa.
func adzunaLocation(display string, area []string) string {
	if display != "" {
		return clean(display)
	}
	if n := len(area); n > 0 {
		return area[n-1]
	}
	return ""
}
