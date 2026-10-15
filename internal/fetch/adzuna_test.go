package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdzunaFetch(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if r.URL.Path != "/v1/api/jobs/es/search/1" || q.Get("app_id") != "ID" || q.Get("app_key") != "KEY" ||
			q.Get("where") != "Madrid" || q.Get("sort_by") != "date" || q.Get("max_days_old") != "14" {
			t.Errorf("petición inesperada: %s", r.URL)
		}
		// La misma oferta sale en las dos búsquedas: no debe duplicarse.
		w.Write([]byte(`{"count":2,"results":[
		  {"id":"111","title":"<strong>Integradora social</strong> - Centro de menores",
		   "company":{"display_name":"Fundación Ejemplo"},
		   "location":{"display_name":"Getafe, Comunidad de Madrid","area":["España","Comunidad de Madrid","Getafe"]},
		   "description":"Buscamos <strong>integradora social</strong> con experiencia…",
		   "redirect_url":"https://www.adzuna.es/details/111","created":"2026-10-14T09:00:00Z"},
		  {"id":"222","title":"Técnico/a en integración social","company":{},
		   "location":{"area":["España","Comunidad de Madrid","Madrid"]},
		   "redirect_url":"https://www.adzuna.es/details/222","created":"2026-10-13T09:00:00Z"}
		]}`))
	}))
	defer srv.Close()

	f := &Adzuna{AppID: "ID", AppKey: "KEY", Where: "Madrid", Queries: []string{"integradora social", "integracion social"}, BaseURL: srv.URL}
	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(jobs) != 2 {
		t.Fatalf("esperaba 2 llamadas y 2 ofertas sin duplicar; llamadas=%d ofertas=%d", calls, len(jobs))
	}
	j := jobs[0]
	if j.Title != "Integradora social - Centro de menores" || j.Company != "Fundación Ejemplo" ||
		j.Location != "Getafe, Comunidad de Madrid" || j.Key() != "adzuna:111" || strings.Contains(j.Summary, "<strong>") {
		t.Errorf("oferta mal parseada: %+v", j)
	}
	if jobs[1].Location != "Madrid" {
		t.Errorf("sin display_name debería usar el último área: %+v", jobs[1])
	}
}

func TestAdzunaErrorDoesNotLeakKeys(t *testing.T) {
	// Servidor cerrado: error de red, que en net/http incluye la URL completa.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	f := &Adzuna{AppID: "SECRETID", AppKey: "SECRETKEY", Queries: []string{"x"}, BaseURL: srv.URL}
	_, err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("esperaba error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("el error filtra las claves: %v", err)
	}
}
