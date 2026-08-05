package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
)

const version = "0.0.0"

func usage() {
	fmt.Fprintf(os.Stderr, `workctl %s - shared CLI for developer-service APIs

Usage:
  workctl [global options] raw METHOD PATH [--data BODY]
  workctl [global options] MODULE ...
  workctl version

Global options:
  --config FILE
  --url URL
  --token-env NAME
  --timeout SECONDS
  --insecure
`, version)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("workctl", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file")
	baseURL := fs.String("url", "", "API base URL")
	tokenEnv := fs.String("token-env", "", "environment variable holding the token")
	timeout := fs.Int("timeout", 0, "HTTP timeout in seconds")
	insecure := fs.Bool("insecure", false, "disable TLS verification")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "--help" || rest[0] == "-h" {
		usage()
		return nil
	}
	if rest[0] == "version" {
		fmt.Println(version)
		return nil
	}
	if description, ok := plannedModules[rest[0]]; ok {
		_ = description
		return cmdPlanned(rest[0])
	}
	if rest[0] != "raw" {
		return fmt.Errorf("unknown command %q", rest[0])
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if *baseURL != "" {
		cfg.URL = *baseURL
	}
	if *tokenEnv != "" {
		cfg.TokenEnv = *tokenEnv
	}
	if *timeout > 0 {
		cfg.TimeoutSeconds = *timeout
	}
	if *insecure {
		cfg.VerifyTLS = false
	}
	if len(rest) < 3 {
		return fmt.Errorf("usage: workctl raw METHOD PATH [--data BODY]")
	}
	raw := flag.NewFlagSet("raw", flag.ContinueOnError)
	data := raw.String("data", "", "request body")
	if err := raw.Parse(rest[3:]); err != nil {
		return err
	}
	client, err := newAPIClient(cfg)
	if err != nil {
		return err
	}
	method := rest[1]
	if method == "" {
		method = http.MethodGet
	}
	b, err := client.do(context.Background(), method, rest[2], []byte(*data))
	if err != nil {
		return err
	}
	return printBody(b)
}
