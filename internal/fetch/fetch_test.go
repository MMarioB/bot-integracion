package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const bocmFixture = `<?xml version="1.0" encoding="ISO-8859-1"?>
<rss version="2.0"><channel>
<title>BOCM - Boletín del día</title>
<item>
  <title>Resoluci` + "\xf3" + `n por la que se convoca bolsa de T` + "\xe9" + `cnico Superior en Integraci` + "\xf3" + `n Social</title>
  <link>https://www.bocm.es/boletin/CM_Orden_BOCM/2026/10/15/BOCM-20261015-1.PDF</link>
  <guid>BOCM-20261015-1</guid>
  <description>&lt;p&gt;Ayuntamiento de Fuenlabrada&lt;/p&gt;</description>
  <pubDate>Thu, 15 Oct 2026 07:00:00 +0200</pubDate>
</item>
<item>
  <title>Licitación de obras de asfaltado</title>
  <link>https://www.bocm.es/x2</link>
</item>
</channel></rss>`

func TestRSSFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(bocmFixture))
	}))
	defer srv.Close()

	f := &RSS{SourceName: "bocm", URL: srv.URL, Location: "Comunidad de Madrid"}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("esperaba 2 items, hay %d", len(jobs))
	}
	j := jobs[0]
	if j.Title != "Resolución por la que se convoca bolsa de Técnico Superior en Integración Social" {
		t.Errorf("título mal decodificado: %q", j.Title)
	}
	if j.ID != "BOCM-20261015-1" || j.Summary != "Ayuntamiento de Fuenlabrada" {
		t.Errorf("campos inesperados: %+v", j)
	}
	if j.Published.IsZero() {
		t.Error("fecha sin parsear")
	}
	if jobs[1].ID != "https://www.bocm.es/x2" {
		t.Errorf("sin guid debería usar el link como ID, got %q", jobs[1].ID)
	}
}

func TestInfoJobsFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "id" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/9/offer" || r.URL.Query().Get("province") != "madrid" {
			t.Errorf("petición inesperada: %s", r.URL)
		}
		w.Write([]byte(`{"offers":[{"id":"abc","title":"Integradora social - centro de día",
			"city":"Getafe","province":{"value":"Madrid"},"link":"https://www.infojobs.net/x/of-iabc",
			"author":{"name":"Fundación Ejemplo"},"published":"2026-10-14T09:00:00.000Z",
			"requirementMin":"Título TSIS"}]}`))
	}))
	defer srv.Close()

	f := &InfoJobs{ClientID: "id", ClientSecret: "secret", Queries: []string{"integradora social"},
		Province: "madrid", BaseURL: srv.URL}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("esperaba 1 oferta, hay %d", len(jobs))
	}
	j := jobs[0]
	if j.Key() != "infojobs:abc" || j.Company != "Fundación Ejemplo" || j.Location != "Getafe (Madrid)" {
		t.Errorf("oferta inesperada: %+v", j)
	}

	bad := &InfoJobs{ClientID: "x", ClientSecret: "y", Queries: []string{"a"}, BaseURL: srv.URL}
	if _, err := bad.Fetch(context.Background()); err == nil {
		t.Error("esperaba error con credenciales malas")
	}
}
