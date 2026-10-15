package filter

import (
	"testing"

	"github.com/mario/ofertas-bot/internal/job"
)

func TestMatch(t *testing.T) {
	f := New([]string{"integradora social", "Integración Social"}, []string{"voluntariado"})
	cases := []struct {
		title, summary string
		want           bool
	}{
		{"INTEGRADORA SOCIAL para centro de menores", "", true},
		{"Bolsa de Técnico Superior en Integracion Social", "", true},
		{"Educador/a", "Se valora titulación en integración social", true},
		{"Integradora social", "Puesto de voluntariado", false},
		{"Camarero/a", "", false},
	}
	for _, c := range cases {
		got := f.Match(job.Job{Title: c.title, Summary: c.summary})
		if got != c.want {
			t.Errorf("Match(%q, %q) = %v, quería %v", c.title, c.summary, got, c.want)
		}
	}
}
