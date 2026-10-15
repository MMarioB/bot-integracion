// ofertas-bot busca ofertas de integración social en Madrid y avisa por Telegram.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/mario/ofertas-bot/internal/fetch"
	"github.com/mario/ofertas-bot/internal/filter"
	"github.com/mario/ofertas-bot/internal/job"
	"github.com/mario/ofertas-bot/internal/notify"
	"github.com/mario/ofertas-bot/internal/store"
)

type Config struct {
	Keywords []string `json:"keywords"`
	Exclude  []string `json:"exclude"`
	InfoJobs struct {
		Enabled  bool     `json:"enabled"`
		Province string   `json:"province"`
		Queries  []string `json:"queries"`
	} `json:"infojobs"`
	Hacesfalta struct {
		Enabled  bool     `json:"enabled"`
		Province string   `json:"province"`
		Queries  []string `json:"queries"`
	} `json:"hacesfalta"`
	BOE struct {
		Enabled bool `json:"enabled"`
		Days    int  `json:"days"`
	} `json:"boe"`
	RSS []struct {
		Name                 string `json:"name"`
		URL                  string `json:"url"`
		Location             string `json:"location"`
		Label                string `json:"label"`
		TitleFromDescription bool   `json:"title_from_description"`
	} `json:"rss"`
	Bulletins []struct {
		Name         string `json:"name"`
		URL          string `json:"url"`
		Location     string `json:"location"`
		Label        string `json:"label"`
		MaxBulletins int    `json:"max_bulletins"`
	} `json:"bulletins"`
	StatePath    string `json:"state_path"`
	StateMaxDays int    `json:"state_max_days"`
}

func main() {
	cfgPath := flag.String("config", "config.json", "ruta al fichero de configuración")
	once := flag.Bool("once", false, "ejecuta una pasada y termina (para cron / GitHub Actions)")
	interval := flag.Duration("interval", 2*time.Hour, "cada cuánto buscar en modo continuo")
	dryRun := flag.Bool("dry-run", false, "imprime por consola en vez de enviar a Telegram y no guarda estado")
	digest := flag.Bool("digest", false, "envía un resumen con TODAS las ofertas publicadas ahora (incluidas las ya avisadas)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	var n notify.Notifier = notify.Stdout{}
	if !*dryRun {
		token, chat := os.Getenv("TELEGRAM_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID")
		if token == "" || chat == "" {
			log.Error("faltan TELEGRAM_TOKEN y/o TELEGRAM_CHAT_ID (o usa -dry-run)")
			os.Exit(1)
		}
		n = &notify.Telegram{Token: token, ChatID: chat}
	}

	st, err := store.Open(cfg.StatePath)
	if err != nil {
		log.Error("estado", "err", err)
		os.Exit(1)
	}

	app := &App{
		log:      log,
		fetchers: buildFetchers(cfg, log),
		filter:   filter.New(cfg.Keywords, cfg.Exclude),
		store:    st,
		notifier: n,
		dryRun:   *dryRun,
		digest:   *digest,
		maxAge:   time.Duration(cfg.StateMaxDays) * 24 * time.Hour,
	}
	if len(app.fetchers) == 0 {
		log.Error("no hay ninguna fuente activa en la configuración")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app.RunOnce(ctx)
	if *once {
		return
	}
	t := time.NewTicker(*interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("saliendo")
			return
		case <-t.C:
			app.RunOnce(ctx)
		}
	}
}

type App struct {
	log      *slog.Logger
	fetchers []job.Fetcher
	filter   *filter.Filter
	store    *store.Store
	notifier notify.Notifier
	dryRun   bool
	digest   bool // resumen completo en la próxima pasada
	maxAge   time.Duration
}

// RunOnce consulta todas las fuentes en paralelo, filtra, deduplica y notifica.
func (a *App) RunOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var (
		mu   sync.Mutex
		all  []job.Job
		wg   sync.WaitGroup
		errs []string
	)
	for _, f := range a.fetchers {
		wg.Add(1)
		go func(f job.Fetcher) {
			defer wg.Done()
			jobs, err := f.Fetch(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				a.log.Warn("fuente con error", "fuente", f.Name(), "err", err)
				errs = append(errs, f.Name())
				return
			}
			a.log.Info("fuente ok", "fuente", f.Name(), "ofertas", len(jobs))
			all = append(all, jobs...)
		}(f)
	}
	wg.Wait()

	var fresh []job.Job
	dup := map[string]bool{}
	for _, j := range all {
		k := j.Key()
		if dup[k] || (!a.digest && a.store.Seen(k)) || !a.filter.Match(j) {
			continue
		}
		dup[k] = true
		fresh = append(fresh, j)
	}
	sort.Slice(fresh, func(i, k int) bool { return fresh[i].Published.Before(fresh[k].Published) })

	// Primera ejecución: en vez de un mensaje por oferta, un resumen agrupado
	// con todo lo que hay publicado ahora mismo.
	if (a.store.FirstRun() || a.digest) && !a.dryRun {
		a.digest = false
		for _, msg := range notify.FormatDigest(fresh) {
			if err := a.notifier.HTML(ctx, msg); err != nil {
				a.log.Error("telegram", "err", err)
				return // sin guardar estado: se reintenta la bienvenida en la siguiente pasada
			}
		}
		for _, j := range fresh {
			a.store.Mark(j.Key())
		}
		a.log.Info("resumen inicial enviado", "ofertas", len(fresh))
		a.save()
		return
	}

	// Fuente recién añadida: lo que ya tenía publicado va en un resumen, no oferta a oferta.
	if !a.dryRun {
		fresh = a.digestNewSources(ctx, fresh)
	}

	sent := 0
	for _, j := range fresh {
		if err := a.notifier.Job(ctx, j); err != nil {
			a.log.Error("no se pudo notificar", "oferta", j.Key(), "err", err)
			continue // no la marcamos: se reintenta en la siguiente pasada
		}
		a.store.Mark(j.Key())
		sent++
	}
	a.log.Info("pasada terminada", "nuevas", sent, "fuentes_con_error", errs)
	if !a.dryRun {
		a.save()
	}
}

// digestNewSources manda un resumen por cada fuente de la que aún no se ha guardado
// nada y devuelve el resto de ofertas para el flujo normal.
func (a *App) digestNewSources(ctx context.Context, fresh []job.Job) []job.Job {
	bySource := map[string][]job.Job{}
	var order []string
	var rest []job.Job
	for _, j := range fresh {
		if a.store.KnowsSource(j.Source) {
			rest = append(rest, j)
			continue
		}
		if _, ok := bySource[j.Source]; !ok {
			order = append(order, j.Source)
		}
		bySource[j.Source] = append(bySource[j.Source], j)
	}
	for _, src := range order {
		jobs := bySource[src]
		if len(jobs) == 1 { // una sola oferta: mejor como alerta normal
			rest = append(rest, jobs...)
			continue
		}
		ok := true
		for _, msg := range notify.FormatSourceDigest(notify.SourceLabel(src), jobs) {
			if err := a.notifier.HTML(ctx, msg); err != nil {
				a.log.Error("telegram", "err", err)
				ok = false
				break
			}
		}
		if !ok {
			continue // se reintenta en la siguiente pasada
		}
		for _, j := range jobs {
			a.store.Mark(j.Key())
		}
		a.log.Info("resumen de fuente nueva enviado", "fuente", src, "ofertas", len(jobs))
	}
	return rest
}

func (a *App) save() {
	if err := a.store.Save(a.maxAge); err != nil {
		a.log.Error("guardando estado", "err", err)
	}
}

func buildFetchers(cfg *Config, log *slog.Logger) []job.Fetcher {
	httpc := &http.Client{Timeout: 30 * time.Second}
	var fs []job.Fetcher

	if cfg.InfoJobs.Enabled {
		id, secret := os.Getenv("INFOJOBS_CLIENT_ID"), os.Getenv("INFOJOBS_CLIENT_SECRET")
		if id == "" || secret == "" {
			log.Warn("InfoJobs activado pero faltan INFOJOBS_CLIENT_ID/INFOJOBS_CLIENT_SECRET; se omite")
		} else {
			fs = append(fs, &fetch.InfoJobs{
				ClientID: id, ClientSecret: secret,
				Queries: cfg.InfoJobs.Queries, Province: cfg.InfoJobs.Province,
				HTTP: httpc,
			})
		}
	}
	if cfg.Hacesfalta.Enabled {
		fs = append(fs, &fetch.Hacesfalta{
			Queries: cfg.Hacesfalta.Queries, Province: cfg.Hacesfalta.Province, HTTP: httpc,
		})
	}
	if cfg.BOE.Enabled {
		fs = append(fs, &fetch.BOE{Days: cfg.BOE.Days, HTTP: httpc})
	}
	for _, b := range cfg.Bulletins {
		fs = append(fs, &fetch.Bulletin{
			SourceName: b.Name, FeedURL: b.URL, Location: b.Location,
			Label: b.Label, MaxBulletins: b.MaxBulletins, HTTP: httpc,
		})
	}
	for _, r := range cfg.RSS {
		fs = append(fs, &fetch.RSS{
			SourceName: r.Name, URL: r.URL, Location: r.Location,
			Label: r.Label, TitleFromDescription: r.TitleFromDescription,
			HTTP: httpc,
		})
	}
	return fs
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.StatePath == "" {
		c.StatePath = "data/seen.json"
	}
	if c.StateMaxDays == 0 {
		c.StateMaxDays = 120
	}
	return &c, nil
}
