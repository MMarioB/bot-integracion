package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mario/ofertas-bot/internal/job"
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

func TestRSSTitleFromDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel>
<title>Ordenes ultimo boletin</title>
<item><title>Orden Nº 30 / Boletín Nº 246 año 2026</title><link>https://www.bocm.es/o30</link>
<description>AYUNTAMIENTO DE GETAFE. Bolsa de Técnico Superior en Integración Social</description></item>
</channel></rss>`))
	}))
	defer srv.Close()

	f := &RSS{SourceName: "bocm", URL: srv.URL, Label: "BOCM", TitleFromDescription: true}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j := jobs[0]
	if j.Title != "AYUNTAMIENTO DE GETAFE. Bolsa de Técnico Superior en Integración Social" {
		t.Errorf("el título debería venir de la descripción: %q", j.Title)
	}
	if j.Summary != "Orden Nº 30 / Boletín Nº 246 año 2026" || j.Company != "BOCM" {
		t.Errorf("resumen/entidad inesperados: %+v", j)
	}
}

const hfFixture = `<html><body>
<div class="oferta">
  <a href="/oportunidades/remuneradas/detalle/integradora-social-centro-de-dia/278600"><img src="logo.png"></a>
  <h3><a href="/oportunidades/remuneradas/detalle/integradora-social-centro-de-dia/278600">Integradora social centro de d&iacute;a</a></h3>
  <p class="ong">Fundaci&oacute;n Ejemplo</p> |
  <p class="lugar">Espa&ntilde;a(Madrid)</p>
  <p>Buscamos integradora social para centro de d&iacute;a...</p>
  <a href="/oportunidades/remuneradas/interes/278600">Me interesa</a>
</div>
<div class="oferta">
  <h3><a href="https://www.hacesfalta.org/oportunidades/remuneradas/detalle/integrador-social-pisos-tutelados/278601?volver=1">Integrador/a social pisos tutelados</a></h3>
  <p>Asociaci&oacute;n Otra</p><p>España (Barcelona)</p>
</div>
<div class="oferta">
  <h3><a href="/oportunidades/remuneradas/detalle/tecnicoa-integracion-social/278602">Técnico/a integración social</a></h3>
  <p>Cruz Roja Española</p><p>España (Alcalá de Henares, Madrid)</p>
</div>
<a href="Default.aspx?pageIndex=2">2</a>
</body></html>`

func TestHacesfaltaFetch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oportunidades/remuneradas/Default.aspx" {
			t.Errorf("ruta inesperada %s", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("clave")
		w.Write([]byte(hfFixture))
	}))
	defer srv.Close()

	f := &Hacesfalta{Queries: []string{"integrador social", "integradora social"}, Province: "Madrid", BaseURL: srv.URL}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "integradora social" {
		t.Errorf("parámetro clave inesperado: %q", gotQuery)
	}
	// Barcelona fuera; las dos de Madrid dentro y sin duplicar entre búsquedas.
	if len(jobs) != 2 {
		t.Fatalf("esperaba 2 ofertas de Madrid, hay %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.ID != "278600" || j.Title != "Integradora social centro de día" || j.Location != "Madrid" ||
		j.Company != "Fundación Ejemplo" || j.URL != srv.URL+"/oportunidades/remuneradas/detalle/integradora-social-centro-de-dia/278600" {
		t.Errorf("oferta mal parseada: %+v", j)
	}
	if jobs[1].Location != "Alcalá de Henares, Madrid" || jobs[1].Company != "Cruz Roja Española" {
		t.Errorf("segunda oferta mal parseada: %+v", jobs[1])
	}
}

func TestBulletinFetch(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/rss", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>BOAM</title>
<item><title>10234,BOAM de 15 de octubre de 2026</title><link>` + srvURL + `/boletin?id=10234&amp;c=1</link>
<guid>b10234</guid><pubDate>Thu, 15 Oct 2026 00:00:00 +0200</pubDate></item>
<item><title>10233,BOAM de 14 de octubre de 2026</title><link>` + srvURL + `/boletin?id=10233</link>
<guid>b10233</guid><pubDate>Wed, 14 Oct 2026 00:00:00 +0200</pubDate></item>
<item><title>10232,BOAM de 13 de octubre de 2026</title><link>` + srvURL + `/viejo</link>
<guid>b10232</guid><pubDate>Tue, 13 Oct 2026 00:00:00 +0200</pubDate></item>
</channel></rss>`))
	})
	mux.HandleFunc("/boletin", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "10234" {
			// Enlace con título largo + enlace corto con el título justo delante.
			w.Write([]byte(`<html><head><title>x</title><script>var a="<a href='/js'>no</a>";</script></head><body>
<nav><a href="/">Inicio</a> <a href="#top">Arriba</a></nav>
<ul>
<li><a href="/anuncios/1.pdf">Resolución por la que se aprueban las bases de la bolsa de Técnico Superior en Integración Social</a></li>
<li><span>Decreto de nombramiento de personal funcionario interino en el Distrito de Usera</span> <a href="/anuncios/2.pdf">PDF</a></li>
</ul></body></html>`))
			return
		}
		// Boletín en ISO-8859-1: "Integración"
		w.Write([]byte("<html><body><p>Convocatoria de mediador intercultural para Servicios Sociales, Integraci\xf3n</p> <a href='/anuncios/3.pdf'>Descargar</a></body></html>"))
	})
	mux.HandleFunc("/viejo", func(w http.ResponseWriter, r *http.Request) {
		t.Error("no debería pedir boletines más allá de MaxBulletins")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	f := &Bulletin{SourceName: "boam", FeedURL: srv.URL + "/rss", Label: "BOAM", Location: "Madrid (ciudad)", MaxBulletins: 2}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byURL := map[string]job.Job{}
	for _, j := range jobs {
		byURL[strings.TrimPrefix(j.URL, srv.URL)] = j
	}
	if len(jobs) != 3 {
		t.Fatalf("esperaba 3 anuncios (menú fuera), hay %d: %+v", len(jobs), jobs)
	}
	if j := byURL["/anuncios/1.pdf"]; !strings.Contains(j.Title, "Integración Social") || j.Company != "BOAM · nº 10234 (15 de octubre de 2026)" {
		t.Errorf("anuncio 1: %+v", j)
	}
	if j := byURL["/anuncios/2.pdf"]; j.Title != "Decreto de nombramiento de personal funcionario interino en el Distrito de Usera" {
		t.Errorf("anuncio 2 debería tomar el texto previo: %q", j.Title)
	}
	if j := byURL["/anuncios/3.pdf"]; !strings.Contains(j.Title, "Integración") {
		t.Errorf("anuncio 3 (latin1) mal decodificado: %q", j.Title)
	}
}
