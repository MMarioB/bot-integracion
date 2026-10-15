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
