package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ardvi/harness/mcp/internal/catalog"
	"github.com/ardvi/harness/mcp/internal/hub"
	"github.com/ardvi/harness/mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ardvi install | init | update | service ensure|status|stop | skills list | hook <event> --client claude|codex | inbox --session ID | codex-bridge --session ID --project UUID --thread ID | serve")
}
func localHost(value string) bool {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func safeHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			http.Error(w, "local Host required", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !localHost(u.Host) {
				http.Error(w, "local Origin required", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func serve(args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := f.String("listen", "127.0.0.1:8765", "loopback listen address")
	data := f.String("data", "", "data directory")
	catalogPath := f.String("catalog", "", "catalog path")
	allowNonLoopback := f.Bool("allow-non-loopback", false, "allow container-internal non-loopback listener")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !localHost(*listen) && (!*allowNonLoopback || os.Getenv("ARDVI_CONTAINER") != "1") {
		return fmt.Errorf("listen address must be loopback")
	}
	if *data == "" || *catalogPath == "" {
		return fmt.Errorf("--data and --catalog are required")
	}
	s, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer s.Close()
	c, err := catalog.Load(*catalogPath)
	if err != nil {
		return err
	}
	server := hub.New(s, c, version)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 2 << 20, PropagateRequestCancellation: true}))
	httpServer := &http.Server{Addr: *listen, Handler: safeHTTP(mux), ReadHeaderTimeout: 5 * time.Second}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-stopped; _ = httpServer.Close() }()
	log.Printf("Ardvi MCP listening on http://%s/mcp", *listen)
	err = httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func memory(args []string, importing bool) error {
	f := flag.NewFlagSet("memory", flag.ContinueOnError)
	data := f.String("data", "", "data directory")
	project := f.String("project", "", "project UUID")
	file := f.String("file", "", "JSONL file")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *data == "" || *project == "" || *file == "" {
		return errors.New("--data, --project, and --file are required")
	}
	s, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer s.Close()
	if importing {
		var items []store.Memory
		if *file == "-" {
			items, err = store.ReadExportFrom(os.Stdin)
		} else {
			items, err = store.ReadExport(*file)
		}
		if err != nil {
			return err
		}
		return s.ImportMemory(*project, items)
	}
	if *file == "-" {
		return store.WriteExportTo(os.Stdout, s.ExportMemory(*project))
	}
	return store.WriteExport(*file, s.ExportMemory(*project))
}
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = installRuntime(os.Args[2:], false)
	case "init":
		err = projectInit(os.Args[2:])
	case "project":
		if len(os.Args) < 3 || os.Args[2] != "init" {
			usage()
			os.Exit(2)
		}
		err = projectInit(os.Args[3:])
	case "update":
		err = installRuntime(os.Args[2:], true)
	case "service":
		err = serviceCommand(os.Args[2:])
	case "skills":
		err = skillsCommand(os.Args[2:])
	case "memory":
		err = memoryCommand(os.Args[2:])
	case "healthcheck":
		err = healthcheck(os.Args[2:])
	case "hook":
		err = hookCommand(os.Args[2:])
	case "inbox":
		err = inboxCommand(os.Args[2:])
	case "codex-bridge":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		err = runCodexBridge(ctx, os.Args[2:])
		stop()
	case "serve":
		err = serve(os.Args[2:])
	case "memory-export":
		err = memory(os.Args[2:], false)
	case "memory-import":
		err = memory(os.Args[2:], true)
	case "version", "--version":
		fmt.Printf("ardvi %s (%s)\n", version, commit)
		return
	default:
		usage()
		os.Exit(2)
	}
	var wake *hookWake
	if errors.As(err, &wake) {
		os.Exit(2) // Claude asyncRewake treats exit 2 as a native wake signal.
	}
	if err != nil {
		log.Fatal(err)
	}
}
