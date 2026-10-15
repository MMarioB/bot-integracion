// Package mailalert lee los correos de alertas de empleo (InfoJobs, Indeed, LinkedIn…)
// de un buzón IMAP y los convierte en ofertas.
package mailalert

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// imapClient es un cliente IMAP4rev1 mínimo: solo lo necesario para leer correos
// recientes sin marcarlos como leídos (BODY.PEEK), así siguen "sin leer" para quien
// abra el buzón.
type imapClient struct {
	conn net.Conn
	r    *bufio.Reader
	tag  int
}

// dialFunc permite sustituir la conexión en los tests.
type dialFunc func(ctx context.Context, addr string) (net.Conn, error)

func dialTLS(ctx context.Context, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 20 * time.Second}, Config: &tls.Config{ServerName: host}}
	return d.DialContext(ctx, "tcp", addr)
}

func connect(ctx context.Context, dial dialFunc, addr string) (*imapClient, error) {
	conn, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	} else {
		conn.SetDeadline(time.Now().Add(90 * time.Second))
	}
	c := &imapClient{conn: conn, r: bufio.NewReader(conn)}
	greeting, err := c.r.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("saludo IMAP: %w", err)
	}
	if !strings.HasPrefix(greeting, "* OK") {
		conn.Close()
		return nil, fmt.Errorf("saludo IMAP inesperado: %q", strings.TrimSpace(greeting))
	}
	return c, nil
}

func (c *imapClient) Close() error { return c.conn.Close() }

// response es la respuesta a un comando: líneas no etiquetadas y literales asociados.
type response struct {
	lines    []string
	literals [][]byte // en orden de aparición
}

var literalRe = regexp.MustCompile(`\{(\d+)\}\r?\n?$`)

// cmd envía un comando y lee hasta su respuesta etiquetada.
// Nunca incluye el comando en los errores (puede llevar la contraseña).
func (c *imapClient) cmd(format string, args ...any) (*response, error) {
	c.tag++
	tag := "A" + strconv.Itoa(c.tag)
	if _, err := fmt.Fprintf(c.conn, tag+" "+format+"\r\n", args...); err != nil {
		return nil, err
	}
	resp := &response{}
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		// Literal {n}: los siguientes n bytes son datos (p. ej. el correo completo).
		for {
			m := literalRe.FindStringSubmatch(line)
			if m == nil {
				break
			}
			n, _ := strconv.Atoi(m[1])
			if n > 25<<20 {
				return nil, fmt.Errorf("literal IMAP demasiado grande (%d bytes)", n)
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(c.r, buf); err != nil {
				return nil, err
			}
			resp.literals = append(resp.literals, buf)
			rest, err := c.r.ReadString('\n') // resto de la línea tras el literal, p. ej. ")"
			if err != nil {
				return nil, err
			}
			line = strings.TrimRight(line, "\r\n") + "<literal>" + rest
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, tag+" ") {
			status := strings.TrimPrefix(line, tag+" ")
			if strings.HasPrefix(status, "OK") {
				return resp, nil
			}
			return nil, &imapError{status: status}
		}
		resp.lines = append(resp.lines, line)
	}
}

type imapError struct{ status string }

func (e *imapError) Error() string { return "IMAP: " + e.status }

// quote escapa una cadena como quoted string de IMAP.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func (c *imapClient) login(user, pass string) error {
	if strings.ContainsAny(user+pass, "\r\n") {
		return errors.New("usuario o contraseña con saltos de línea")
	}
	_, err := c.cmd("LOGIN %s %s", quote(user), quote(pass))
	if err != nil {
		var ie *imapError
		if errors.As(err, &ie) {
			return fmt.Errorf("login rechazado (¿contraseña de aplicación correcta?): %s", ie.status)
		}
		return err
	}
	return nil
}

func (c *imapClient) selectMailbox(name string) error {
	_, err := c.cmd("EXAMINE %s", quote(name)) // solo lectura
	return err
}

// searchSince devuelve los UID de los correos recibidos desde la fecha dada.
func (c *imapClient) searchSince(since time.Time) ([]string, error) {
	resp, err := c.cmd("UID SEARCH SINCE %s", since.Format("2-Jan-2006"))
	if err != nil {
		return nil, err
	}
	var uids []string
	for _, l := range resp.lines {
		if strings.HasPrefix(l, "* SEARCH") {
			uids = append(uids, strings.Fields(strings.TrimPrefix(l, "* SEARCH"))...)
		}
	}
	return uids, nil
}

// fetch descarga los correos completos sin marcarlos como leídos.
func (c *imapClient) fetch(uids []string) ([][]byte, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	resp, err := c.cmd("UID FETCH %s (BODY.PEEK[])", strings.Join(uids, ","))
	if err != nil {
		return nil, err
	}
	return resp.literals, nil
}

func (c *imapClient) logout() {
	c.cmd("LOGOUT") // el servidor cierra; el error da igual
}
