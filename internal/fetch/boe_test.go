package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBOEFetch(t *testing.T) {
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/datosabiertos/api/boe/sumario/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("falta Accept: application/json")
		}
		switch strings.TrimPrefix(r.URL.Path, "/datosabiertos/api/boe/sumario/") {
		case "20261015":
			// epigrafe con "item" como array; departamento como array.
			w.Write([]byte(`{"status":{"code":"200"},"data":{"sumario":{"diario":[{"seccion":[
			  {"codigo":"1","departamento":[{"nombre":"X","epigrafe":[{"item":{"identificador":"BOE-A-1","titulo":"Integración social en Madrid (no es 2B)"}}]}]},
			  {"codigo":"2B","departamento":[
			    {"nombre":"ADMINISTRACIÓN LOCAL","epigrafe":[{"nombre":"Personal","item":[
			      {"identificador":"BOE-A-2026-100","titulo":"Resolución de 1 de octubre de 2026, del Ayuntamiento de Getafe (Madrid), referente a la convocatoria para proveer varias plazas.",
			       "url_html":"https://www.boe.es/diario_boe/txt.php?id=BOE-A-2026-100","url_xml":"` + base + `/xml/100"},
			      {"identificador":"BOE-A-2026-101","titulo":"Resolución del Ayuntamiento de Sevilla, referente a la convocatoria de Técnico en Integración Social.",
			       "url_xml":"` + base + `/xml/no-deberia-pedirse"}
			    ]}]},
			    {"nombre":"MINISTERIO DE JUVENTUD E INFANCIA","epigrafe":{"item":{"identificador":"BOE-A-2026-102","titulo":"Orden por la que se convoca proceso selectivo de integradores sociales."}}}
			  ]}
			]}]}}}`))
		case "20261014":
			w.WriteHeader(http.StatusNotFound) // no hubo BOE
		case "20261013":
			// departamento como objeto con "item" directo, sin epigrafe.
			w.Write([]byte(`{"data":{"sumario":{"diario":{"seccion":{"codigo":"2B","departamento":{"nombre":"COMUNIDAD DE MADRID",
			  "item":{"identificador":"BOE-A-2026-090","titulo":"Resolución de la Consejería de Familia, por la que se convoca bolsa.","url_xml":"` + base + `/xml/90"}}}}}}}`))
		default:
			t.Errorf("día inesperado %s", r.URL.Path)
		}
	})
	mux.HandleFunc("/xml/100", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><documento><metadatos/><texto><p>En el BOCM se publican las bases para cubrir dos plazas de <b>Técnico Superior en Integración Social</b>, personal laboral.</p></texto></documento>`))
	})
	mux.HandleFunc("/xml/90", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // falla la descarga del texto
	})
	mux.HandleFunc("/xml/no-deberia-pedirse", func(w http.ResponseWriter, r *http.Request) {
		t.Error("no debería descargar textos de fuera de Madrid")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	f := &BOE{Days: 3, BaseURL: srv.URL, Now: func() time.Time {
		return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	}}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for i, j := range jobs {
		got[j.ID] = i
	}
	if len(jobs) != 3 {
		t.Fatalf("esperaba Getafe, Ministerio y Comunidad (3); hay %d: %+v", len(jobs), jobs)
	}
	if _, ok := got["BOE-A-2026-101"]; ok {
		t.Error("Sevilla no debería colarse")
	}
	g := jobs[got["BOE-A-2026-100"]]
	if !strings.Contains(g.Summary, "Técnico Superior en Integración Social") || g.Location != "Comunidad de Madrid" ||
		g.Company != "BOE · Administración local" || g.URL != "https://www.boe.es/diario_boe/txt.php?id=BOE-A-2026-100" {
		t.Errorf("Getafe mal: %+v", g)
	}
	m := jobs[got["BOE-A-2026-102"]]
	if m.Location != "Administración del Estado" || m.Summary != "" {
		t.Errorf("Ministerio mal: %+v", m)
	}
	c := jobs[got["BOE-A-2026-090"]]
	if c.Summary != "" || !strings.HasSuffix(c.URL, "id=BOE-A-2026-090") {
		t.Errorf("si falla el texto debe quedarse con el título y un enlace válido: %+v", c)
	}
}

func TestBOEAllDaysMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	f := &BOE{Days: 2, BaseURL: srv.URL}
	if _, err := f.Fetch(context.Background()); err == nil {
		t.Error("si no hay ningún sumario en varios días debería avisar con un error")
	}
}
