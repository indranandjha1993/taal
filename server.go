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

	mux.Handle("/", http.FileServer(http.FS(sub)))
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := fs.ReadFile(sub, name)
			if err != nil {
				http.Error(w, "missing "+name, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		}
	}
	mux.HandleFunc("/host", page("host.html"))
	mux.HandleFunc("/join", page("join.html"))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/sources", s.handleSources)
	mux.HandleFunc("/outputs", s.handleOutputs)
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

func (s *server) handleSources(w http.ResponseWriter, r *http.Request) {
	devs, err := s.cap.devices()
	if err != nil {
		http.Error(w, "cannot list audio devices", http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{"name": d.Name, "loopback": d.Loop})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(marshal(out))
}

// where the mac sends its audio. picking a capture source is useless if the
// mac is still playing straight out of its speakers, so taal owns both ends
// rather than sending people into Audio MIDI Setup.
func (s *server) handleOutputs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		name := r.URL.Query().Get("name")
		if !setOutput(name) {
			http.Error(w, "could not switch output", http.StatusBadRequest)
			return
		}
		s.broadcast(map[string]any{"type": "output", "name": currentOutputName()})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(marshal(outputDevices()))
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
