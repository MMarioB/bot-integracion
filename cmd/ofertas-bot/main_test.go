package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mario/ofertas-bot/internal/filter"
	"github.com/mario/ofertas-bot/internal/job"
	"github.com/mario/ofertas-bot/internal/store"
)

type fakeFetcher struct {
	name string
	jobs []job.Job
}

func (f *fakeFetcher) Name() string                             { return f.name }
func (f *fakeFetcher) Fetch(context.Context) ([]job.Job, error) { return f.jobs, nil }

type fakeNotifier struct {
	jobs    []string
	digests []string
}

func (n *fakeNotifier) Job(_ context.Context, j job.Job) error {
	n.jobs = append(n.jobs, j.Key())
	return nil
}
func (n *fakeNotifier) Text(_ context.Context, m string) error {
	n.digests = append(n.digests, m)
	return nil
}
func (n *fakeNotifier) HTML(_ context.Context, m string) error {
	n.digests = append(n.digests, m)
	return nil
}

func offer(src, id string) job.Job {
	return job.Job{Source: src, ID: id, Title: "Integradora social " + id, URL: "https://x/" + id}
}

func newApp(t *testing.T, st *store.Store, n *fakeNotifier, fs ...job.Fetcher) *App {
	t.Helper()
	return &App{
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		fetchers: fs,
		filter:   filter.New([]string{"integradora social"}, nil),
		store:    st,
		notifier: n,
		maxAge:   24 * time.Hour,
	}
}

func TestNewSourceGoesToDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")

	// 1) Bot recién arrancado solo con BOCM: resumen de bienvenida.
	st, _ := store.Open(path)
	n := &fakeNotifier{}
	bocm := &fakeFetcher{name: "bocm", jobs: []job.Job{offer("bocm", "1")}}
	newApp(t, st, n, bocm).RunOnce(context.Background())
	if len(n.digests) != 1 || !strings.Contains(n.digests[0], "Soy tu buscador") || len(n.jobs) != 0 {
		t.Fatalf("arranque: digests=%v jobs=%v", n.digests, n.jobs)
	}

	// 2) Se añade Hacesfalta con 3 ofertas vigentes y sale 1 nueva en BOCM:
	//    Hacesfalta va en resumen de fuente nueva, BOCM como alerta normal.
	st, _ = store.Open(path)
	n = &fakeNotifier{}
	bocm.jobs = append(bocm.jobs, offer("bocm", "2"))
	hf := &fakeFetcher{name: "hacesfalta", jobs: []job.Job{offer("hacesfalta", "a"), offer("hacesfalta", "b"), offer("hacesfalta", "c")}}
	newApp(t, st, n, bocm, hf).RunOnce(context.Background())
	if len(n.digests) != 1 || !strings.Contains(n.digests[0], "Ahora también busco en Hacesfalta.org") ||
		!strings.Contains(n.digests[0], "3 ofertas abiertas") {
		t.Fatalf("fuente nueva: digests=%v", n.digests)
	}
	if len(n.jobs) != 1 || n.jobs[0] != "bocm:2" {
		t.Fatalf("alertas normales inesperadas: %v", n.jobs)
	}

	// 3) Siguiente pasada: nada repetido; una oferta nueva de Hacesfalta llega como alerta.
	st, _ = store.Open(path)
	n = &fakeNotifier{}
	hf.jobs = append(hf.jobs, offer("hacesfalta", "d"))
	newApp(t, st, n, bocm, hf).RunOnce(context.Background())
	if len(n.digests) != 0 || len(n.jobs) != 1 || n.jobs[0] != "hacesfalta:d" {
		t.Fatalf("tercera pasada: digests=%v jobs=%v", n.digests, n.jobs)
	}
}

// Una alerta de Indeed trae de todo (cajeros, limpieza, textos del pie): solo debe llegar
// lo que pasa el filtro de palabras clave. Las de InfoJobs (fiables) llegan tal cual.
func TestPortalAlertsFiltering(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "seen.json"))
	n := &fakeNotifier{}
	alerts := &fakeFetcher{name: "email", jobs: []job.Job{
		{Source: "indeed", ID: "1", Title: "Cajero/a - Reponedor/a - Villarejo de Salvanés 30h/Rotativo"},
		{Source: "indeed", ID: "2", Title: "Técnico/a de Integración Mecánica y Estructural"},
		{Source: "indeed", ID: "3", Title: "desde ayer"},
		{Source: "indeed", ID: "4", Title: "Crea tu CV Indeed"},
		{Source: "indeed", ID: "5", Title: "Integradora social - piso tutelado"},
		{Source: "infojobs", ID: "of-i1", Title: "Educador/a centro de menores", Prefiltered: true},
	}}
	app := newApp(t, st, n, alerts)
	app.filter = filter.New([]string{"integradora social", "integracion social"}, []string{"voluntariado"})
	app.store.Mark("bocm:x") // bot ya arrancado
	app.RunOnce(context.Background())

	sent := strings.Join(append(append([]string{}, n.jobs...), n.digests...), "\n")
	for _, bad := range []string{"Cajero", "Mecánica", "desde ayer", "Crea tu CV"} {
		if strings.Contains(sent, bad) {
			t.Errorf("no debería haberse enviado %q:\n%s", bad, sent)
		}
	}
	for _, good := range []string{"Integradora social - piso tutelado", "Educador/a centro de menores"} {
		if !strings.Contains(sent, good) {
			t.Errorf("debería haberse enviado %q:\n%s", good, sent)
		}
	}
}
