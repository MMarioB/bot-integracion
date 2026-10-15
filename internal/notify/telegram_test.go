package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mario/ofertas-bot/internal/job"
)

func TestTelegramSend(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("ruta inesperada %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tg := &Telegram{Token: "TOKEN", ChatID: "42", BaseURL: srv.URL}
	err := tg.Job(context.Background(), job.Job{
		Source: "infojobs", Title: "Integradora <social> & más", URL: "https://x?a=1&b=2",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := got["text"].(string)
	if got["chat_id"] != "42" || got["parse_mode"] != "HTML" {
		t.Errorf("payload inesperado: %v", got)
	}
	if !strings.Contains(text, "Integradora &lt;social&gt; &amp; más") || !strings.Contains(text, `href="https://x?a=1&amp;b=2"`) {
		t.Errorf("escape HTML incorrecto: %s", text)
	}
}

func TestFormatDigest(t *testing.T) {
	if msgs := FormatDigest(nil); len(msgs) != 1 || !strings.Contains(msgs[0], "no hay nada abierto") {
		t.Errorf("resumen vacío inesperado: %v", msgs)
	}

	var jobs []job.Job
	for i := 0; i < 60; i++ {
		jobs = append(jobs, job.Job{
			Title: strings.Repeat("Integradora social & ", 4), Company: "Fundación <X>",
			Location: "Madrid", URL: "https://example.com/oferta?id=1&x=2", Source: "infojobs",
		})
	}
	msgs := FormatDigest(jobs)
	if len(msgs) < 2 {
		t.Fatalf("60 ofertas largas deberían partirse en varios mensajes, hay %d", len(msgs))
	}
	total := 0
	for _, m := range msgs {
		if len([]rune(m)) > 4096 {
			t.Errorf("mensaje de %d caracteres supera el límite de Telegram", len([]rune(m)))
		}
		total += strings.Count(m, "<a href=")
		if strings.Contains(m, "<X>") || strings.Contains(m, "&x=2") {
			t.Error("contenido sin escapar en el resumen")
		}
	}
	if total != 60 {
		t.Errorf("se esperaban 60 ofertas en el resumen, hay %d", total)
	}
	if !strings.Contains(msgs[len(msgs)-1], "Sin prisa") {
		t.Error("falta el pie tranquilizador en el último mensaje")
	}
	if !strings.Contains(msgs[0], "<b>60 ofertas abiertas</b>") {
		t.Errorf("cabecera inesperada: %s", msgs[0][:100])
	}
}
