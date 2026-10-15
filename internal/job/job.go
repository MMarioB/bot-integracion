// Package job define el modelo común de oferta y la interfaz que implementa cada fuente.
package job

import (
	"context"
	"time"
)

// Job es una oferta normalizada, venga de la fuente que venga.
type Job struct {
	ID        string // único y estable dentro de su fuente
	Source    string // "infojobs", "bocm", ...
	Title     string
	Company   string
	Location  string
	URL       string
	Summary   string
	Published time.Time
	// Prefiltered: la fuente ya filtró por la búsqueda (p. ej. una alerta guardada en un
	// portal); solo se aplican las exclusiones, no las palabras clave.
	Prefiltered bool
}

// Key es el identificador global usado para la deduplicación.
func (j Job) Key() string { return j.Source + ":" + j.ID }

// Fetcher obtiene ofertas de una fuente concreta.
type Fetcher interface {
	Name() string
	Fetch(ctx context.Context) ([]Job, error)
}
