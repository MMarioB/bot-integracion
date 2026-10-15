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
	RSS []struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Location string `json:"location"`
	} `json:"rss"`
	StatePath    string `json:"state_path"`
	StateMaxDays int    `json:"state_max_days"`
}

func main() {
	cfgPath := flag.String("config", "config.json", "ruta al fichero de configuración")
	once := flag.Bool("once", false, "ejecuta una pasada y termina (para cron / GitHub Actions)")
	interval := flag.Duration("interval", 2*time.Hour, "cada cuánto buscar en modo continuo")
	dryRun := flag.Bool("dry-run", false, "imprime por consola en vez de enviar a Telegram y no guarda estado")
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
		if dup[k] || a.store.Seen(k) || !a.filter.Match(j) {
			continue
		}
		dup[k] = true
		fresh = append(fresh, j)
	}
	sort.Slice(fresh, func(i, k int) bool { return fresh[i].Published.Before(fresh[k].Published) })

	// Primera ejecución: no inundamos el chat con todo lo que ya está publicado.
	if a.store.FirstRun() && !a.dryRun {
		for _, j := range fresh {
			a.store.Mark(j.Key())
		}
		msg := fmt.Sprintf("👋 Bot de ofertas en marcha. Hay %d ofertas activas que encajan; a partir de ahora te aviso solo de las nuevas.", len(fresh))
		if err := a.notifier.Text(ctx, msg); err != nil {
			a.log.Error("telegram", "err", err)
		}
		a.save()
		return
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
	for _, r := range cfg.RSS {
		fs = append(fs, &fetch.RSS{SourceName: r.Name, URL: r.URL, Location: r.Location, HTTP: httpc})
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
