package mailalert

import (
	"context"
	"fmt"
	"time"

	"github.com/mario/ofertas-bot/internal/job"
)

// Fetcher lee los correos de alertas de los últimos días de un buzón IMAP
// (p. ej. un Gmail creado solo para esto, con contraseña de aplicación).
// No marca nada como leído ni borra nada: el buzón queda tal cual.
type Fetcher struct {
	Addr     string // "imap.gmail.com:993"
	User     string
	Password string
	Mailbox  string // "INBOX" por defecto
	Days     int    // correos de los últimos N días (por defecto 3)

	dial dialFunc         // tests
	now  func() time.Time // tests
}

func (f *Fetcher) Name() string { return "email" }

func (f *Fetcher) Fetch(ctx context.Context) ([]job.Job, error) {
	dial := f.dial
	if dial == nil {
		dial = dialTLS
	}
	now := time.Now
	if f.now != nil {
		now = f.now
	}
	mailbox := f.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	days := f.Days
	if days <= 0 {
		days = 3
	}

	c, err := connect(ctx, dial, f.Addr)
	if err != nil {
		return nil, fmt.Errorf("email: conectando a %s: %w", f.Addr, err)
	}
	defer c.Close()
	if err := c.login(f.User, f.Password); err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}
	defer c.logout()
	if err := c.selectMailbox(mailbox); err != nil {
		return nil, fmt.Errorf("email: abriendo %s: %w", mailbox, err)
	}
	uids, err := c.searchSince(now().AddDate(0, 0, -days))
	if err != nil {
		return nil, fmt.Errorf("email: buscando correos: %w", err)
	}
	raws, err := c.fetch(uids)
	if err != nil {
		return nil, fmt.Errorf("email: descargando correos: %w", err)
	}

	var out []job.Job
	for _, raw := range raws {
		msg, err := ParseMessage(raw)
		if err != nil {
			continue // un correo raro no debe tumbar la fuente entera
		}
		jobs, ok := ExtractJobs(msg)
		if !ok {
			continue // no es de un portal conocido (bienvenidas, Google…)
		}
		out = append(out, jobs...)
	}
	return out, nil
}
