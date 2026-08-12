package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/transport"
)

type readyMessage struct {
	Schema  string `json:"schema"`
	URL     string `json:"url"`
	Version string `json:"version"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "loopback listen address")
	account := flag.String("account", "", "Gmail account used for draft headers")
	tokenPath := flag.String("token", "", "path to Google OAuth token.json")
	credentialsPath := flag.String(
		"credentials",
		"",
		"path to Google OAuth credentials.json; defaults beside token.json",
	)
	eventsPath := flag.String(
		"events-file",
		"",
		"optional NDJSON file for local transport events",
	)
	secret := flag.String(
		"secret",
		os.Getenv("CWAPI_TRANSPORT_SECRET"),
		"local bearer secret; normally injected by CWapi runtime",
	)
	flag.Parse()

	if *account == "" || *tokenPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *secret == "" {
		log.Fatal("CWAPI_TRANSPORT_SECRET is required")
	}
	if *credentialsPath == "" {
		*credentialsPath = filepath.Join(filepath.Dir(*tokenPath), "credentials.json")
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatalf("invalid listen address: %v", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		log.Fatal("cwapi-transport only listens on loopback addresses")
	}

	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	actualHost, actualPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		log.Fatalf("resolve listener address: %v", err)
	}
	if actualHost == "::1" {
		actualHost = "[::1]"
	}
	baseURL := "http://" + net.JoinHostPort(stringsTrimBrackets(actualHost), actualPort)
	if actualHost == "[::1]" {
		baseURL = "http://[::1]:" + actualPort
	}

	network := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	baseClient := &http.Client{Transport: network, Timeout: 30 * time.Second}
	events := transport.NewEventLog(256, *eventsPath)
	health := transport.NewHealthState(events)
	client := transport.NewResilientClient(baseClient, health)
	tokens := transport.NewTokenManager(*tokenPath, client)
	tokens.SetHealthState(health)
	gmail := transport.NewGmailClient(*account, client, tokens)
	oauth := transport.NewOAuthManager(*credentialsPath, tokens, client, events)
	cachePath := filepath.Clean(filepath.Join(
		filepath.Dir(*tokenPath),
		"..",
		"state",
		"gmail-draft-cache-go.json",
	))
	if err := gmail.SetDraftCachePath(cachePath); err != nil {
		log.Printf("Gmail draft cache disabled until rewritten: %v", err)
	}
	transportServer := transport.NewServer(gmail, *secret, health, events)
	transportServer.SetOAuthManager(oauth)
	server := &http.Server{
		Handler:           transportServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	events.Append(
		"transport_started",
		"INFO",
		"Go Gmail transport service started.",
		map[string]any{"listen": listener.Addr().String(), "version": transport.Version},
	)
	if err := json.NewEncoder(os.Stdout).Encode(readyMessage{
		Schema:  "cwapi.transport.ready.v1",
		URL:     baseURL,
		Version: transport.Version,
	}); err != nil {
		log.Fatalf("write readiness: %v", err)
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func stringsTrimBrackets(value string) string {
	if len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']' {
		return value[1 : len(value)-1]
	}
	return value
}
