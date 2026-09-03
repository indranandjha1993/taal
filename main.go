package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

func main() {
	// 8225 is "taal" typed on a phone keypad
	port := flag.Int("port", 8225, "port to listen on")
	plain := flag.Bool("http", false, "serve plain http, which disables the screen wake lock")
	flag.Parse()

	cap, err := newCapture()
	if err != nil {
		log.Fatal(err)
	}
	defer cap.close()

	s := newServer(cap)

	// in a container the detected address belongs to the container, which
	// nothing on the wifi can reach, so allow it to be told
	addr := os.Getenv("TAAL_HOST")
	if addr == "" {
		addr = lanIP()
	}

	scheme := "https"
	if *plain {
		scheme = "http"
	}

	fmt.Println()
	fmt.Println("  taal is up")
	fmt.Printf("  host controls   %s://%s:%d/host\n", scheme, addr, *port)
	fmt.Printf("  speakers join   %s://%s:%d/\n", scheme, addr, *port)
	printSources(cap)
	if os.Getenv("TAAL_HOST") == "" && inContainer() {
		fmt.Println()
		fmt.Println("  that address is probably wrong inside a container.")
		fmt.Println("  set TAAL_HOST to this machine's wifi address.")
	}
	fmt.Println()

	listen := fmt.Sprintf(":%d", *port)
	if *plain {
		fmt.Println("  plain http: phones cannot hold the screen on.")
		fmt.Println()
		log.Fatal(http.ListenAndServe(listen, s.routes()))
	}

	certFile, keyFile, err := ensureCert(addr)
	if err != nil {
		log.Fatal("certificate: ", err)
	}

	fmt.Println("  self signed cert, so each phone warns once on first visit.")
	fmt.Println("  accept it and the screen wake lock starts working.")
	fmt.Println()

	srv := &http.Server{
		Addr:    listen,
		Handler: s.routes(),
		// go answers a plain http request to a tls port with a bare line of
		// text, which reads as a broken page. send a redirect instead.
		ErrorLog: log.New(tlsNoise{}, "", 0),
	}
	log.Fatal(serveTLSWithRedirect(srv, certFile, keyFile, *port))
}

// a mic would capture the room, so say plainly whether a loopback device
// exists before the user goes looking for the source list
func printSources(c *capture) {
	devs, err := c.devices()
	if err != nil {
		return
	}
	var loop []string
	for _, d := range devs {
		if d.Loop {
			loop = append(loop, d.Name)
		}
	}
	fmt.Println()
	if len(loop) == 0 {
		fmt.Println("  no loopback audio device found.")
		fmt.Println("  install blackhole to capture what this mac is playing:")
		fmt.Println("  brew install blackhole-2ch")
		return
	}
	fmt.Println("  capture ready:", strings.Join(loop, ", "))
}

func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// set by the image, so the startup hint only appears where it applies
func inContainer() bool {
	return os.Getenv("TAAL_IN_CONTAINER") == "1"
}

// first private IPv4 on an up interface, so the join URL works on
// home Wi-Fi and phone hotspots alike (no internet needed)
func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "localhost"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 != nil && ip4.IsPrivate() {
				return ip4.String()
			}
		}
	}
	return "localhost"
}
