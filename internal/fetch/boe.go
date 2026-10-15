package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // zona Europe/Madrid aunque el runner no tenga tzdata

	"github.com/mario/ofertas-bot/internal/filter"
	"github.com/mario/ofertas-bot/internal/job"
)

// BOE lee el sumario oficial del BOE (API de datos abiertos) de los últimos días, sección
// II.B "Oposiciones y concursos". Ahí publican sus convocatorias el Ayuntamiento de Madrid,
// el resto de municipios y la Comunidad. Como muchos títulos son genéricos ("referente a la
// convocatoria para proveer varias plazas"), para los anuncios de Madrid se descarga también
// el texto, y así el filtro de palabras clave puede buscar dentro.
type BOE struct {
	Days    int    // días hacia atrás a revisar, incluido hoy (por defecto 3)
	BaseURL string // vacío = https://www.boe.es
	HTTP    *http.Client
	Now     func() time.Time // para tests
}

func (f *BOE) Name() string { return "boe" }

// oneOrMany acepta tanto un objeto como un array de objetos (herencia del XML).
type oneOrMany[T any] []T

func (o *oneOrMany[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '[' {
		var s []T
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*o = s
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*o = []T{v}
	return nil
}

type boeItem struct {
	Identificador string `json:"identificador"`
	Titulo        string `json:"titulo"`
	URLHTML       string `json:"url_html"`
	URLXML        string `json:"url_xml"`
}

type boeEpigrafe struct {
	Nombre string             `json:"nombre"`
	Item   oneOrMany[boeItem] `json:"item"`
}

type boeDepartamento struct {
	Nombre   string                 `json:"nombre"`
	Epigrafe oneOrMany[boeEpigrafe] `json:"epigrafe"`
	Item     oneOrMany[boeItem]     `json:"item"`
}

type boeSeccion struct {
	Codigo       string                     `json:"codigo"`
	Departamento oneOrMany[boeDepartamento] `json:"departamento"`
}

type boeSumario struct {
	Data struct {
		Sumario struct {
			Diario oneOrMany[struct {
				Seccion oneOrMany[boeSeccion] `json:"seccion"`
			}] `json:"diario"`
		} `json:"sumario"`
	} `json:"data"`
}

func (f *BOE) Fetch(ctx context.Context) ([]job.Job, error) {
	base := strings.TrimRight(f.BaseURL, "/")
	if base == "" {
		base = "https://www.boe.es"
	}
	days := f.Days
	if days <= 0 {
		days = 3
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	madrid, _ := time.LoadLocation("Europe/Madrid")
	today := now().In(madrid)

	var out []job.Job
	ok := 0
	for d := 0; d < days; d++ {
		day := today.AddDate(0, 0, -d)
		sum, found, err := f.sumario(ctx, base, day.Format("20060102"))
		if err != nil {
			return nil, err
		}
		if !found { // domingos y festivos no hay BOE
			continue
		}
		ok++
		for _, diario := range sum.Data.Sumario.Diario {
			for _, sec := range diario.Seccion {
				if sec.Codigo != "2B" {
					continue
				}
				for _, dep := range sec.Departamento {
					items := append([]boeItem{}, dep.Item...)
					for _, ep := range dep.Epigrafe {
						items = append(items, ep.Item...)
					}
					for _, it := range items {
						j, keep := f.toJob(ctx, it, dep.Nombre, day)
						if keep {
							out = append(out, j)
						}
					}
				}
			}
		}
	}
	if ok == 0 {
		return nil, fmt.Errorf("boe: no se pudo leer ningún sumario de los últimos %d días", days)
	}
	return out, nil
}

// toJob decide si el anuncio interesa por ubicación y, si es de Madrid, baja su texto.
func (f *BOE) toJob(ctx context.Context, it boeItem, dep string, day time.Time) (job.Job, bool) {
	title := clean(it.Titulo)
	nt, nd := filter.Normalize(title), filter.Normalize(dep)

	isMadrid := strings.Contains(nt, "(madrid)") || strings.Contains(nt, "ayuntamiento de madrid") ||
		strings.Contains(nt, "comunidad de madrid") || strings.Contains(nd, "comunidad de madrid")
	isState := strings.HasPrefix(nd, "ministerio")
	if !isMadrid && !isState {
		return job.Job{}, false
	}

	loc := "Comunidad de Madrid"
	if isState && !isMadrid {
		loc = "Administración del Estado"
	}

	summary := ""
	if isMadrid && it.URLXML != "" {
		// Si falla, nos quedamos con el título: mejor eso que perder el anuncio.
		summary, _ = f.texto(ctx, it.URLXML)
	}

	link := it.URLHTML
	if link == "" {
		link = "https://www.boe.es/diario_boe/txt.php?id=" + it.Identificador
	}
	return job.Job{
		ID:        it.Identificador,
		Source:    f.Name(),
		Title:     title,
		Company:   strings.TrimSpace("BOE · " + titleCase(dep)),
		Location:  loc,
		URL:       link,
		Summary:   summary,
		Published: day,
	}, true
}

func (f *BOE) sumario(ctx context.Context, base, date string) (*boeSumario, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/datosabiertos/api/boe/sumario/"+date, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ofertas-bot/0.1")
	resp, err := client(f.HTTP).Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("boe %s: %w", date, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("boe %s: HTTP %d", date, resp.StatusCode)
	}
	var s boeSumario
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, false, fmt.Errorf("boe %s: %w", date, err)
	}
	return &s, true, nil
}

var textoRe = regexp.MustCompile(`(?is)<texto[^>]*>(.*?)</texto>`)

// texto descarga el XML de un anuncio y devuelve su cuerpo en texto plano.
func (f *BOE) texto(ctx context.Context, u string) (string, error) {
	if strings.HasPrefix(u, "/") {
		u = "https://www.boe.es" + u
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ofertas-bot/0.1")
	resp, err := client(f.HTTP).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if m := textoRe.FindSubmatch(b); m != nil {
		return clean(string(m[1])), nil
	}
	return "", nil
}

func client(c *http.Client) *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c
}

// "ADMINISTRACIÓN LOCAL" -> "Administración local"
func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(strings.ToLower(s))
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}
