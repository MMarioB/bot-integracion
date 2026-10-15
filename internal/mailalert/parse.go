package mailalert

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mario/ofertas-bot/internal/filter"
	"github.com/mario/ofertas-bot/internal/job"
)

// portal describe cómo reconocer los correos de un portal y sus enlaces a ofertas.
type portal struct {
	source  string   // nombre de la fuente ("infojobs")
	senders []string // dominios del remitente
	hosts   []string // dominios a los que apuntan los enlaces de ofertas (incluido el de tracking)
}

var portals = []portal{
	{source: "infojobs", senders: []string{"infojobs.net"}, hosts: []string{"infojobs.net"}},
	{source: "indeed", senders: []string{"indeed.com", "indeedemail.com"}, hosts: []string{"indeed.com"}},
	{source: "linkedin", senders: []string{"linkedin.com"}, hosts: []string{"linkedin.com"}},
}

// Textos de enlaces que no son ofertas (menús, pie, baja…). Normalizados, sin tildes.
var noiseText = []string{
	"darte de baja", "darse de baja", "dejar de recibir", "unsubscribe", "baja de",
	"privacidad", "cookies", "condiciones", "aviso legal", "ayuda", "help",
	"preferencias", "configura", "gestionar", "editar alerta", "modificar alerta", "tus alertas",
	"ver mas", "ver todas", "ver todos", "mas ofertas", "see all", "view all",
	"descarga la app", "app store", "google play", "perfil", "iniciar sesion", "accede",
	"facebook", "twitter", "instagram", "youtube", "tiktok",
}

// Message es un correo de alerta ya decodificado.
type Message struct {
	From    string
	Subject string
	Date    time.Time
	HTML    string
	Text    string
}

// ParseMessage decodifica un correo completo (RFC 5322 + MIME).
func ParseMessage(raw []byte) (*Message, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	dec := new(mime.WordDecoder)
	dec.CharsetReader = charsetReader
	subject, err := dec.DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		subject = m.Header.Get("Subject")
	}
	from := m.Header.Get("From")
	if addr, err := (&mail.AddressParser{WordDecoder: dec}).Parse(from); err == nil {
		from = addr.Address
	}
	date, _ := m.Header.Date()

	msg := &Message{From: strings.ToLower(from), Subject: subject, Date: date}
	if err := walkPart(m.Header, m.Body, msg, 0); err != nil {
		return nil, err
	}
	return msg, nil
}

// header lo cumplen tanto mail.Header como textproto.MIMEHeader.
type header interface{ Get(string) string }

func walkPart(h header, body io.Reader, msg *Message, depth int) error {
	if depth > 10 {
		return nil
	}
	ctype := h.Get("Content-Type")
	if ctype == "" {
		ctype = "text/plain; charset=utf-8"
	}
	mediaType, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart() // Raw: decodificamos nosotros (también base64)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := walkPart(p.Header, p, msg, depth+1); err != nil {
				return err
			}
		}
	}
	if mediaType != "text/html" && mediaType != "text/plain" {
		return nil // adjuntos, imágenes…
	}
	data, err := io.ReadAll(decodeTransfer(h.Get("Content-Transfer-Encoding"), body))
	if err != nil {
		return err
	}
	text := toUTF8(data, params["charset"])
	if mediaType == "text/html" && msg.HTML == "" {
		msg.HTML = text
	} else if mediaType == "text/plain" && msg.Text == "" {
		msg.Text = text
	}
	return nil
}

func decodeTransfer(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &newlineStripper{r: r})
	}
	return r
}

// newlineStripper quita saltos de línea para el decodificador base64.
type newlineStripper struct{ r io.Reader }

func (n *newlineStripper) Read(p []byte) (int, error) {
	k, err := n.r.Read(p)
	j := 0
	for _, b := range p[:k] {
		if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
			p[j] = b
			j++
		}
	}
	return j, err
}

func charsetReader(charset string, r io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(toUTF8(b, charset)), nil
}

// toUTF8 convierte ISO-8859-1/Windows-1252 a UTF-8; el resto se deja tal cual.
func toUTF8(b []byte, charset string) string {
	cs := strings.ToLower(charset)
	latin := cs == "iso-8859-1" || cs == "latin1" || cs == "windows-1252" || cs == "cp1252" || cs == "iso-8859-15"
	if latin || (cs == "" && !utf8.Valid(b)) {
		out := make([]byte, 0, len(b)+len(b)/8)
		for _, c := range b {
			out = utf8.AppendRune(out, rune(c))
		}
		return string(out)
	}
	return string(b)
}

// portalFor identifica el portal por el remitente.
func portalFor(from string) (portal, bool) {
	at := strings.LastIndexByte(from, '@')
	domain := from[at+1:]
	for _, p := range portals {
		for _, s := range p.senders {
			if domain == s || strings.HasSuffix(domain, "."+s) {
				return p, true
			}
		}
	}
	return portal{}, false
}

var (
	anchorOpenRe = regexp.MustCompile(`(?i)<a\b`)
	anchorRe     = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	dropBlockRe  = regexp.MustCompile(`(?is)<(style|script|head)\b.*?</(style|script|head)>`)
	tagRe        = regexp.MustCompile(`<[^>]*>`)
	spaceRe      = regexp.MustCompile(`\s+`)
)

func cleanText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// ExtractJobs saca las ofertas de un correo de alerta. Devuelve ok=false si el correo
// no es de un portal conocido.
//
// Estrategia genérica (válida mientras no haya un parser específico por portal):
// enlaces al dominio del portal cuyo texto parece un título de oferta. El texto que
// sigue al enlace hasta el siguiente enlace de oferta se usa como entidad/ubicación.
func ExtractJobs(msg *Message) ([]job.Job, bool) {
	p, ok := portalFor(msg.From)
	if !ok {
		return nil, false
	}
	page := dropBlockRe.ReplaceAllString(msg.HTML, " ")

	type link struct {
		href, title string
		start, end  int
	}
	var links []link
	seenTitle := map[string]bool{}
	for _, m := range anchorRe.FindAllStringSubmatchIndex(page, -1) {
		href := html.UnescapeString(strings.TrimSpace(page[m[2]:m[3]]))
		title := cleanText(page[m[4]:m[5]])
		if !hostMatches(href, p.hosts) || !looksLikeJobTitle(title) {
			continue
		}
		key := filter.Normalize(title)
		if seenTitle[key] { // el mismo título suele enlazarse dos veces (logo + texto)
			continue
		}
		seenTitle[key] = true
		links = append(links, link{href: href, title: title, start: m[0], end: m[1]})
	}

	jobs := make([]job.Job, 0, len(links))
	for i, l := range links {
		end := len(page)
		if i+1 < len(links) {
			end = links[i+1].start
		}
		// La entidad/ubicación es el texto hasta el siguiente enlace, sea o no de oferta
		// (así no se cuela el texto del pie en la última).
		if k := anchorOpenRe.FindStringIndex(page[l.end:end]); k != nil {
			end = l.end + k[0]
		}
		company, location := splitMeta(cleanText(page[l.end:end]))
		jobs = append(jobs, job.Job{
			ID:          stableID(p.source, l.title, company),
			Source:      p.source,
			Title:       l.title,
			Company:     company,
			Location:    location,
			URL:         l.href,
			Published:   msg.Date,
			Prefiltered: true, // el portal ya ha filtrado según la alerta guardada
		})
	}
	return jobs, true
}

func hostMatches(href string, hosts []string) bool {
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, d := range hosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

func looksLikeJobTitle(t string) bool {
	n := len([]rune(t))
	if n < 8 || n > 200 {
		return false
	}
	nt := filter.Normalize(t)
	for _, bad := range noiseText {
		if strings.Contains(nt, bad) {
			return false
		}
	}
	return true
}

// splitMeta intenta separar "Entidad · Ubicación" del texto que sigue al título.
// Es heurístico: si no está claro, deja la entidad con el texto recortado.
func splitMeta(s string) (company, location string) {
	if s == "" {
		return "", ""
	}
	r := []rune(s)
	if len(r) > 120 {
		s = string(r[:120])
		if i := strings.LastIndexByte(s, ' '); i > 60 {
			s = s[:i]
		}
	}
	for _, sep := range []string{" · ", " | ", " - ", " — "} {
		if parts := strings.SplitN(s, sep, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	return s, ""
}

// stableID: los enlaces de los correos llevan tracking distinto en cada envío, así que
// identificamos la oferta por portal + título + entidad para no repetir avisos.
func stableID(source, title, company string) string {
	h := sha1.Sum([]byte(source + "|" + filter.Normalize(title) + "|" + filter.Normalize(company)))
	return hex.EncodeToString(h[:8])
}

// Describe resume un correo para depurar el parser con -parse-eml.
func Describe(msg *Message) string {
	return fmt.Sprintf("De: %s\nAsunto: %s\nFecha: %s\nHTML: %d bytes, texto: %d bytes",
		msg.From, msg.Subject, msg.Date.Format(time.RFC1123Z), len(msg.HTML), len(msg.Text))
}
