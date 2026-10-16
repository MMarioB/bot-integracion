package mailalert

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// Correo de alerta inventado con la forma típica de estos envíos: multipart/alternative,
// HTML en quoted-printable con enlaces de tracking, el mismo título enlazado dos veces,
// y enlaces de pie (baja, privacidad, "ver más") que no son ofertas.
const alertEmail = "From: =?UTF-8?Q?Indeed?= <alert@indeed.com>\r\n" +
	"To: ofertas@example.com\r\n" +
	"Subject: =?UTF-8?Q?3_nuevas_ofertas_de_integrador_social?=\r\n" +
	"Date: Thu, 15 Oct 2026 08:05:00 +0200\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=\"XX\"\r\n" +
	"\r\n" +
	"--XX\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"T2ZlcnRhcyBudWV2YXM=\r\n" +
	"--XX\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"<html><head><style>a{color:red}</style></head><body>=0D\r\n" +
	"<a href=3D\"https://es.indeed.com/t/abc1\"><img src=3D\"logo.png\"></a>=0D\r\n" +
	"<a href=3D\"https://es.indeed.com/t/abc1\">Integradora social - centro de d=C3=ADa</a>=0D\r\n" +
	"<p>Fundaci=C3=B3n Ejemplo &middot; Getafe</p>=0D\r\n" +
	"<a href=3D\"https://es.indeed.com/t/abc2\">T=C3=A9cnico/a de integraci=C3=B3n social=\r\n" +
	" (jornada parcial)</a><p>Asociaci=C3=B3n Otra | Madrid</p>=0D\r\n" +
	"<a href=3D\"https://www.google.com/x\">Enlace externo largo que no es del portal</a>=0D\r\n" +
	"<a href=3D\"https://es.indeed.com/t/more\">Ver m=C3=A1s ofertas</a>=0D\r\n" +
	"<a href=3D\"https://es.indeed.com/t/unsub\">Darte de baja de esta alerta</a>=0D\r\n" +
	"<a href=3D\"https://es.indeed.com/privacidad\">Pol=C3=ADtica de privacidad</a>=0D\r\n" +
	"</body></html>\r\n" +
	"--XX--\r\n"

// Mismas ofertas en un envío posterior con tracking distinto: deben tener el mismo ID.
var alertEmailResend = strings.NewReplacer("abc1", "zzz9", "abc2", "zzz8").Replace(alertEmail)

const welcomeEmail = "From: Google <no-reply@accounts.google.com>\r\n" +
	"Subject: Bienvenida\r\nDate: Thu, 15 Oct 2026 07:00:00 +0200\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n\r\n" +
	"<a href=\"https://google.com/x\">Configura tu cuenta de Google ahora</a>\r\n"

func TestExtractJobs(t *testing.T) {
	msg, err := ParseMessage([]byte(alertEmail))
	if err != nil {
		t.Fatal(err)
	}
	if msg.From != "alert@indeed.com" || msg.Subject != "3 nuevas ofertas de integrador social" || msg.Text != "Ofertas nuevas" {
		t.Errorf("cabeceras/partes mal decodificadas: %+v", msg)
	}
	jobs, ok := ExtractJobs(msg)
	if !ok {
		t.Fatal("debería reconocer el correo como de Indeed")
	}
	if len(jobs) != 2 {
		t.Fatalf("esperaba 2 ofertas (sin logo duplicado, pie ni enlaces externos), hay %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.Source != "indeed" || j.Title != "Integradora social - centro de día" || j.Company != "Fundación Ejemplo" ||
		j.Location != "Getafe" || j.URL != "https://es.indeed.com/t/abc1" || j.Prefiltered {
		t.Errorf("oferta 1 mal: %+v", j)
	}
	if jobs[1].Title != "Técnico/a de integración social (jornada parcial)" || jobs[1].Company != "Asociación Otra" || jobs[1].Location != "Madrid" {
		t.Errorf("oferta 2 mal (soft line break QP, separador |): %+v", jobs[1])
	}

	msg2, _ := ParseMessage([]byte(alertEmailResend))
	jobs2, _ := ExtractJobs(msg2)
	if len(jobs2) != 2 || jobs2[0].Key() != jobs[0].Key() || jobs2[1].Key() != jobs[1].Key() {
		t.Errorf("la misma oferta reenviada con otro tracking debe tener la misma clave")
	}

	w, _ := ParseMessage([]byte(welcomeEmail))
	if _, ok := ExtractJobs(w); ok {
		t.Error("un correo de Google no es una alerta de empleo")
	}
}

func TestLatin1Email(t *testing.T) {
	raw := "From: LinkedIn <jobs-noreply@linkedin.com>\r\nSubject: =?ISO-8859-1?Q?Integraci=F3n?=\r\n" +
		"Content-Type: text/html; charset=ISO-8859-1\r\n\r\n" +
		"<a href=\"https://www.linkedin.com/comm/jobs/view/1\">Mediador intercultural en Legan\xe9s</a>\r\n"
	msg, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := ExtractJobs(msg)
	if msg.Subject != "Integración" || len(jobs) != 1 || jobs[0].Title != "Mediador intercultural en Leganés" {
		t.Errorf("latin1 mal: subject=%q jobs=%+v", msg.Subject, jobs)
	}
}

// fakeIMAP atiende una sesión IMAP mínima por una conexión TCP local.
func fakeIMAP(t *testing.T, mails []string, wantPass string) (addr string, seen *[]string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var cmds []string
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "* OK fake ready\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			tag, rest, _ := strings.Cut(line, " ")
			cmds = append(cmds, rest)
			switch {
			case strings.HasPrefix(rest, "LOGIN"):
				if !strings.Contains(rest, quote(wantPass)) {
					fmt.Fprintf(conn, "%s NO [AUTHENTICATIONFAILED] Invalid credentials\r\n", tag)
					continue
				}
				fmt.Fprintf(conn, "%s OK logged in\r\n", tag)
			case strings.HasPrefix(rest, "EXAMINE"):
				fmt.Fprintf(conn, "* %d EXISTS\r\n%s OK [READ-ONLY] done\r\n", len(mails), tag)
			case strings.HasPrefix(rest, "UID SEARCH"):
				var ids []string
				for i := range mails {
					ids = append(ids, fmt.Sprint(i+1))
				}
				fmt.Fprintf(conn, "* SEARCH %s\r\n%s OK done\r\n", strings.Join(ids, " "), tag)
			case strings.HasPrefix(rest, "UID FETCH"):
				for i, m := range mails {
					fmt.Fprintf(conn, "* %d FETCH (UID %d BODY[] {%d}\r\n%s)\r\n", i+1, i+1, len(m), m)
				}
				fmt.Fprintf(conn, "%s OK done\r\n", tag)
			case strings.HasPrefix(rest, "LOGOUT"):
				fmt.Fprintf(conn, "* BYE\r\n%s OK bye\r\n", tag)
				return
			default:
				fmt.Fprintf(conn, "%s BAD unknown\r\n", tag)
			}
		}
	}()
	return ln.Addr().String(), &cmds
}

func plainDial(ctx context.Context, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
}

func TestFetcherIMAP(t *testing.T) {
	addr, cmds := fakeIMAP(t, []string{welcomeEmail, alertEmail}, "app pass\"x")
	f := &Fetcher{Addr: addr, User: "bot@example.com", Password: "app pass\"x", Days: 3, dial: plainDial,
		now: func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jobs, err := f.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("esperaba 2 ofertas del correo de alerta, hay %d", len(jobs))
	}
	all := strings.Join(*cmds, "\n")
	for _, want := range []string{`LOGIN "bot@example.com" "app pass\"x"`, `EXAMINE "INBOX"`, "UID SEARCH SINCE 12-Oct-2026", "UID FETCH 1,2 (BODY.PEEK[])"} {
		if !strings.Contains(all, want) {
			t.Errorf("falta el comando %q en:\n%s", want, all)
		}
	}
}

func TestFetcherBadPasswordDoesNotLeak(t *testing.T) {
	addr, _ := fakeIMAP(t, nil, "buena")
	f := &Fetcher{Addr: addr, User: "bot@example.com", Password: "SECRETA-mala", dial: plainDial}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := f.Fetch(ctx)
	if err == nil || !strings.Contains(err.Error(), "contraseña de aplicación") {
		t.Fatalf("esperaba error de login claro, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRETA") {
		t.Errorf("el error filtra la contraseña: %v", err)
	}
}

// Correo real de alerta de InfoJobs (anonimizado: sin dirección ni tokens de tracking).
func TestInfoJobsRealAlert(t *testing.T) {
	raw, err := os.ReadFile("testdata/infojobs_alert.eml")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	jobs, ok := ExtractJobs(msg)
	if !ok {
		t.Fatal("debería reconocer el correo de InfoJobs")
	}
	if len(jobs) != 1 {
		t.Fatalf("esperaba solo la oferta (sin 'Eliminar esta alerta', 'menú privado'…), hay %d: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	want := map[string]string{
		"Key":      "infojobs:of-i7ba1011db5402499d77c4133571697",
		"Title":    "Técnico-a de integración social (temporal)",
		"Company":  "AFANIAS",
		"Location": "Madrid",
		"Summary":  "De duración determinada",
	}
	got := map[string]string{"Key": j.Key(), "Title": j.Title, "Company": j.Company, "Location": j.Location, "Summary": j.Summary}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, quería %q", k, got[k], v)
		}
	}
	if !j.Prefiltered {
		t.Error("las alertas de InfoJobs son precisas: deberían ir marcadas como prefiltradas")
	}
	if !strings.HasPrefix(j.URL, "https://link.push.infojobs.net/") {
		t.Errorf("enlace inesperado: %s", j.URL)
	}
}

func TestPrettyTitle(t *testing.T) {
	for in, want := range map[string]string{
		"TÉCNICO-A DE INTEGRACIÓN SOCIAL (TEMPORAL)": "Técnico-a de integración social (temporal)",
		"Integradora social - centro de día":         "Integradora social - centro de día",
		"Educador/a TISOC para CEE":                  "Educador/a TISOC para CEE",
	} {
		if got := prettyTitle(in); got != want {
			t.Errorf("prettyTitle(%q) = %q, quería %q", in, got, want)
		}
	}
}
