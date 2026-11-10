package server

import (
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/fabioconcina/softgaze/internal/store"
)

type Server struct {
	Mux      http.Handler
	Broker   *SSEBroker
	Handlers *Handlers
}

// New builds the Softgaze server: its API, the event stream and the window's
// page (/ goes to /follow.html).
func New(claudeDir string, st *store.Store, webFS fs.FS) *Server {
	broker := NewBroker()
	handlers := NewHandlers(claudeDir, st)
	handlers.events = broker

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", handlers.HandleInfo)
	mux.HandleFunc("GET /api/follow", handlers.HandleFollow)
	mux.HandleFunc("GET /api/follow/sessions", handlers.HandleFollowSessions)
	mux.HandleFunc("GET /api/follow/pin", handlers.HandleFollowPinInfo)
	mux.HandleFunc("POST /api/follow/pin", handlers.HandleFollowPin)
	mux.HandleFunc("POST /api/follow/focus", handlers.HandleFollowFocus)
	mux.HandleFunc("POST /api/follow/back", handlers.HandleFollowBack)
	mux.HandleFunc("POST /api/attention/editor", handlers.HandleAttentionEditor)
	mux.HandleFunc("POST /api/attention/page", handlers.HandleAttentionPage)
	mux.Handle("GET /api/events", broker)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/follow.html", http.StatusFound)
	})
	mux.Handle("/", http.FileServer(http.FS(webFS)))

	return &Server{
		Mux:      secureLocalRequests(mux),
		Broker:   broker,
		Handlers: handlers,
	}
}

// secureLocalRequests adds defense in depth around a server that is meant
// to be used from its own origin on this machine. Direct non-browser clients
// remain supported, but a web page from another origin can neither read
// local data nor trigger process or file mutations.
func secureLocalRequests(next http.Handler) http.Handler {
	var logRejectOnce sync.Once

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}

		// DNS rebinding: a hostile page can point its own domain at
		// 127.0.0.1, which makes it same-origin with this server as far as the
		// browser is concerned, so the Origin check below would pass. The Host
		// header still carries the attacker's domain and a page cannot
		// override it, so only names that really mean this machine get in.
		if !hostAllowed(r.Host) {
			logRejectOnce.Do(func() {
				log.Printf("[server] Rejected request for host %q (logged once)", r.Host)
			})
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !requestIsSameOrigin(r) {
				http.Error(w, "cross-origin request denied", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// requestIsSameOrigin reports whether a request comes from a Softgaze page
// served here: its Origin is this server (plain http, the Host it asked
// for), or it has none and is not marked cross-site.
func requestIsSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Modern browsers identify requests initiated by a different site even
		// in cases where Origin is omitted. CLI/API clients send neither header.
		return !strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site")
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Scheme == "http" && strings.EqualFold(u.Host, r.Host)
}

// hostAllowed reports whether a Host header names this machine: localhost
// or a loopback IP literal. IP literals are safe to accept wholesale because
// DNS rebinding needs a domain name the attacker controls.
func hostAllowed(hostport string) bool {
	host := hostName(hostport)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostName returns the lowercase hostname of a Host header value, without
// port, IPv6 brackets or trailing dot.
func hostName(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.ToLower(host)
}
