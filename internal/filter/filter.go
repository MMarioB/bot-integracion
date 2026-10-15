// Package filter decide qué ofertas son relevantes según palabras clave.
package filter

import (
	"strings"

	"github.com/mario/ofertas-bot/internal/job"
)

// Filter acepta una oferta si contiene alguna palabra de Include
// (en título o resumen) y ninguna de Exclude. No distingue mayúsculas ni tildes.
type Filter struct {
	include []string
	exclude []string
}

func New(include, exclude []string) *Filter {
	return &Filter{include: normAll(include), exclude: normAll(exclude)}
}

func (f *Filter) Match(j job.Job) bool {
	text := Normalize(j.Title + " " + j.Summary)
	for _, ex := range f.exclude {
		if strings.Contains(text, ex) {
			return false
		}
	}
	if len(f.include) == 0 {
		return true
	}
	for _, in := range f.include {
		if strings.Contains(text, in) {
			return true
		}
	}
	return false
}

var accents = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
)

// Normalize pasa a minúsculas y quita tildes ("Integradora" == "integradora").
func Normalize(s string) string {
	return accents.Replace(strings.ToLower(s))
}

func normAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(Normalize(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
