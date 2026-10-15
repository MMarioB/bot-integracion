package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mario/ofertas-bot/internal/job"
)

// InfoJobs consulta la API pública de ofertas (https://developer.infojobs.net).
// Requiere registrar una app para obtener client_id y client_secret.
type InfoJobs struct {
	ClientID     string
	ClientSecret string
	Queries      []string // una búsqueda por término: "integrador social", "integradora social"...
	Province     string   // p. ej. "madrid"
	BaseURL      string   // vacío = https://api.infojobs.net
	HTTP         *http.Client
}

func (f *InfoJobs) Name() string { return "infojobs" }

type ijResponse struct {
	Offers []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		City      string `json:"city"`
		Link      string `json:"link"`
		Published string `json:"published"`
		Province  struct {
			Value string `json:"value"`
		} `json:"province"`
		Author struct {
			Name string `json:"name"`
		} `json:"author"`
		RequirementMin string `json:"requirementMin"`
	} `json:"offers"`
}

func (f *InfoJobs) Fetch(ctx context.Context) ([]job.Job, error) {
	base := f.BaseURL
	if base == "" {
		base = "https://api.infojobs.net"
	}
	var out []job.Job
	for _, q := range f.Queries {
		params := url.Values{}
		params.Set("q", q)
		if f.Province != "" {
			params.Set("province", f.Province)
		}
		params.Set("order", "updated-desc")
		params.Set("maxResults", "50")

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/9/offer?"+params.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(f.ClientID, f.ClientSecret)

		var body ijResponse
		if err := doJSON(f.HTTP, req, &body); err != nil {
			return nil, fmt.Errorf("infojobs %q: %w", q, err)
		}
		for _, o := range body.Offers {
			loc := o.City
			if o.Province.Value != "" && o.Province.Value != o.City {
				loc = o.City + " (" + o.Province.Value + ")"
			}
			published, _ := time.Parse(time.RFC3339, o.Published)
			out = append(out, job.Job{
				ID:        o.ID,
				Source:    f.Name(),
				Title:     o.Title,
				Company:   o.Author.Name,
				Location:  loc,
				URL:       o.Link,
				Summary:   o.RequirementMin,
				Published: published,
			})
		}
	}
	return out, nil
}

func doJSON(c *http.Client, req *http.Request, v any) error {
	if c == nil {
		c = http.DefaultClient
	}
	req.Header.Set("User-Agent", "ofertas-bot/0.1")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
