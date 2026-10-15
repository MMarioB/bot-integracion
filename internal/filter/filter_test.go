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
	// Ofertas ya filtradas por el portal: pasan sin keywords, pero las exclusiones mandan.
	if !f.Match(job.Job{Title: "Educador/a centro de menores", Prefiltered: true}) {
		t.Error("una oferta prefiltrada debería pasar sin palabras clave")
	}
	if f.Match(job.Job{Title: "Integradora social", Summary: "voluntariado", Prefiltered: true}) {
		t.Error("las exclusiones deben aplicarse también a las prefiltradas")
	}
	for _, c := range cases {
		got := f.Match(job.Job{Title: c.title, Summary: c.summary})
		if got != c.want {
			t.Errorf("Match(%q, %q) = %v, quería %v", c.title, c.summary, got, c.want)
		}
	}
}
