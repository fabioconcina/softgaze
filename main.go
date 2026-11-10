// softgaze: a calm window to stay with one Claude Code session while it
// works. It reads the session transcripts and ~/.claude/sessions, and
// serves the window on 127.0.0.1 (port 3133 by default).
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/fabioconcina/softgaze/internal/attention"
	"github.com/fabioconcina/softgaze/internal/config"
	"github.com/fabioconcina/softgaze/internal/parser"
	"github.com/fabioconcina/softgaze/internal/server"
	"github.com/fabioconcina/softgaze/internal/service"
	"github.com/fabioconcina/softgaze/internal/store"
	"github.com/fabioconcina/softgaze/internal/watcher"
)

//go:embed web
var webFS embed.FS

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(version)
			return
		case "service":
			runService()
			return
		}
	}

	openBrowser := flag.Bool("open", false, "Open Softgaze in the browser on startup")
	portOverride := flag.Int("port", 0, "override the port (default 3133)")
	dbOverride := flag.String("db", "", "override the DB path")
	logPath := flag.String("log", "", "append the log to this file (the Windows service has no console)")
	flag.Parse()
	if *logPath != "" {
		logTo(*logPath)
	}
	cfg := config.Load()
	if *portOverride != 0 {
		cfg.Port = *portOverride
	}
	if *dbOverride != "" {
		cfg.DBPath = *dbOverride
	}
	wellbeing := cfg.WellbeingOn()

	log.Printf("[startup] softgaze starting - port=%d claude_dir=%s", cfg.Port, cfg.ClaudeDir)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("[startup] failed to open database: %v", err)
	}
	defer st.Close()

	webContent, _ := fs.Sub(webFS, "web")
	srv := server.New(cfg.ClaudeDir, st, webContent)
	srv.Handlers.Version = version
	srv.Handlers.Wellbeing = wellbeing
	if wellbeing {
		// Which window is in front, sampled only while a Softgaze window is
		// open; a change tells the window to refresh its counter.
		srv.Handlers.Attention = attention.NewSampler(func(string, attention.Verdict) {
			srv.Broker.SendJSON("attention", map[string]string{"reason": "foreground"})
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Broker.Run(ctx)

	// Transcript and PID-file changes: the window refetches the session it
	// shows.
	w, err := watcher.New(cfg.ClaudeDir)
	if err != nil {
		log.Printf("[watcher] Failed to start: %v", err)
	} else {
		w.OnSessionChange(func(path string) {
			id, projectDir := parser.SessionIDForPath(path)
			srv.Broker.SendJSON("session_activity", map[string]string{
				"session_id":  id,
				"project_dir": projectDir,
				"reason":      "transcript",
			})
		})
		w.OnProcessChange(func(path string) {
			id, cwd := parser.SessionFromPIDFile(path)
			srv.Broker.SendJSON("session_activity", map[string]string{
				"session_id": id,
				"cwd":        cwd,
				"reason":     "process",
			})
		})
		go w.Start(ctx)
	}

	// Focus stretches older than 30 days are of no use (the window sums one work period).
	go func() {
		for {
			if err := st.Prune(30); err != nil {
				log.Printf("[prune] Error: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(24 * time.Hour):
			}
		}
	}()

	httpServer := &http.Server{
		// Local only: the window shows session details and can move windows.
		Addr:              fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		Handler:           srv.Mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	go func() {
		log.Printf("[startup] Softgaze available at %s", url)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("[startup] HTTP server error: %v", err)
		}
	}()
	if *openBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			if err := server.OpenURL(url); err != nil {
				log.Printf("[browser] Failed to open: %v", err)
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("[shutdown] stopping...")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer shutdownCancel()
	httpServer.Shutdown(shutdownCtx)
	if w != nil {
		w.Close()
	}
}

func runService() {
	usage := "Usage: softgaze service <install|uninstall|status|restart>"
	if len(os.Args) < 3 {
		fmt.Println(usage)
		os.Exit(1)
	}
	switch os.Args[2] {
	case "install":
		execPath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving executable path: %v\n", err)
			os.Exit(1)
		}
		if err := service.Install(execPath); err != nil {
			fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("softgaze installed and started: http://127.0.0.1:%d\n", config.Load().Port)
		fmt.Println("It starts automatically on login. To stop: softgaze service uninstall")
	case "uninstall":
		if err := service.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "Uninstall failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service stopped and removed from startup.")
	case "status":
		status, err := service.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Service: %s\n", status)
	case "restart":
		if err := service.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "Restart failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service restarted")
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
}

// logTo appends the log, and the report of a crash, to path: where the
// service has no console to write to (Windows).
func logTo(path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Printf("[startup] log file: %v", err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Printf("[startup] log file: %v", err)
		return
	}
	log.SetOutput(f)
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		log.Printf("[startup] crash output: %v", err)
	}
}
