package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Typing "192.168.1.5:8225" into a phone gets you http, and go answers a
// plain http request on a tls port with one line of text that reads as a
// broken page. Peeking at the first byte tells the two apart: a tls
// handshake always starts 0x16, so anything else is http and gets a
// redirect to the same address with https in front.

type peekConn struct {
	net.Conn
	r *bufio.Reader
}

func (c peekConn) Read(b []byte) (int, error) { return c.r.Read(b) }

type splitListener struct {
	net.Listener
	tlsConf *tls.Config
	port    int
	ready   chan net.Conn
	failed  chan error
}

// Peeking blocks until the client sends its first byte, so it must never
// happen on the accept loop: one silent connection would stall every other
// device. Each connection is classified on its own goroutine and the tls
// ones are handed back through a channel.
func (l *splitListener) Accept() (net.Conn, error) {
	for {
		select {
		case c := <-l.ready:
			return c, nil
		case err := <-l.failed:
			return nil, err
		}
	}
}

func (l *splitListener) loop() {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			l.failed <- err
			return
		}
		go l.classify(c)
	}
}

func (l *splitListener) classify(c net.Conn) {
	// a client that connects and says nothing must not hold a slot forever
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	first, err := r.Peek(1)
	if err != nil {
		c.Close()
		return
	}
	c.SetReadDeadline(time.Time{})

	wrapped := peekConn{Conn: c, r: r}
	if first[0] == 0x16 {
		l.ready <- tls.Server(wrapped, l.tlsConf)
		return
	}
	redirect(wrapped, l.port)
}

func redirect(c net.Conn, port int) {
	defer c.Close()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		host = "localhost"
	}
	target := fmt.Sprintf("https://%s:%d%s", host, port, req.URL.RequestURI())

	var b bytes.Buffer
	b.WriteString("HTTP/1.1 302 Found\r\n")
	b.WriteString("Location: " + target + "\r\n")
	b.WriteString("Connection: close\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	c.Write(b.Bytes())
}

func serveTLSWithRedirect(srv *http.Server, certFile, keyFile string, port int) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	conf := &tls.Config{Certificates: []tls.Certificate{cert}}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	sl := &splitListener{
		Listener: ln,
		tlsConf:  conf,
		port:     port,
		ready:    make(chan net.Conn),
		failed:   make(chan error, 1),
	}
	go sl.loop()
	return srv.Serve(sl)
}

// plain http on the tls port is handled by the redirect above, so the
// handshake errors it used to log are noise now. anything else still prints.
type tlsNoise struct{}

func (tlsNoise) Write(p []byte) (int, error) {
	if !strings.Contains(string(p), "TLS handshake error") {
		os.Stderr.Write(p)
	}
	return len(p), nil
}
