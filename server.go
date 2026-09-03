package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed web
var webFS embed.FS

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}

	// One address for everyone. The mac running taal gets the controls,
	// every other device gets the speaker page. Nothing to type, nothing to
	// pick, and a phone cannot reach the controls at all.
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			files.ServeHTTP(w, r)
			return
		}
		name := "join.html"
		if isLoopbackAddr(r.RemoteAddr) {
			name = "host.html"
		}
		b, err := fs.ReadFile(sub, name)
		if err != nil {
			http.Error(w, "missing "+name, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/setup", s.handleSetup)
	mux.HandleFunc("/whoami", s.handleWhoami)
	mux.HandleFunc("/qr.png", s.handleQR)

	return mux
}

// the Host header is whatever the host page was opened with, which is
// localhost as often as not. a QR saying localhost is useless to a phone
func qrTarget(reqHost string) string {
	if env := os.Getenv("TAAL_HOST"); env != "" {
		if _, port, err := net.SplitHostPort(reqHost); err == nil {
			return net.JoinHostPort(env, port)
		}
		return env
	}
	if h, port, err := net.SplitHostPort(reqHost); err == nil && isLocal(h) {
		return net.JoinHostPort(lanIP(), port)
	}
	return reqHost
}

// where the mac sends its audio. picking a capture source is useless if the
// mac is still playing straight out of its speakers, so taal owns both ends
// rather than sending people into Audio MIDI Setup.
// What the host page needs to know before it can offer a start button:
// whether this mac can capture its own audio yet, and if not, how to fix it.
func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		detail, ok := runInstall()
		w.Header().Set("Content-Type", "application/json")
		w.Write(marshal(map[string]any{
			"ok": ok, "detail": detail, "command": installCommand(),
		}))
		return
	}
	setup := inspectAudio(s.cap)
	w.Header().Set("Content-Type", "application/json")
	w.Write(marshal(map[string]any{
		"ready":     setup.Ready,
		"detail":    setup.Detail,
		"installer": setup.Installer,
		"command":   installCommand(),
	}))
}

// Lets the landing page stop asking a question it can answer itself: this
// device either is the mac running taal or it is not, and the stream is
// either live or it is not.
func (s *server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	streaming := s.streaming
	speakers := s.guestCount()
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Write(marshal(map[string]any{
		"host":      isLoopbackAddr(r.RemoteAddr),
		"streaming": streaming,
		"speakers":  speakers,
	}))
}

func (s *server) handleQR(w http.ResponseWriter, r *http.Request) {
	// a qr pointing at http would land phones on a page that cannot hold
	// the screen on, so follow whatever this request came in on
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/", scheme, qrTarget(r.Host))
	png, err := qrcode.Encode(url, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, "qr failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
