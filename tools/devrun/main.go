// Command devrun runs the local development services listed in dev.yaml,
// rebuilding and restarting each one when its source files change.
//
//	go run ./tools/devrun            # every service
//	go run ./tools/devrun api web    # only some
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var colors = []string{"36", "33", "35", "32", "34", "31"}

func main() {
	cfgPath := flag.String("config", "dev.yaml", "services file")
	flag.Parse()
	if err := run(*cfgPath, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "devrun:", err)
		os.Exit(1)
	}
}

func run(cfgPath string, names []string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	services, err := cfg.selectServices(names)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(filepath.Dir(cfgPath))
	if err != nil {
		return err
	}
	if busy := busyPorts(services); len(busy) > 0 {
		return fmt.Errorf("already in use: %s. Stop what is running there (an earlier `make up` or `make dev-*`?) and try again", strings.Join(busy, ", "))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	width := 0
	for _, s := range services {
		width = max(width, len(s.Name))
	}
	var mu sync.Mutex
	color := os.Getenv("NO_COLOR") == ""
	outs := make([]*output, len(services))
	for i, s := range services {
		prefix := fmt.Sprintf("%-*s | ", width, s.Name)
		if color {
			prefix = "\x1b[" + colors[i%len(colors)] + "m" + prefix + "\x1b[0m"
		}
		outs[i] = &output{mu: &mu, w: os.Stdout, prefix: prefix}
	}
	for i, s := range services {
		if s.Port != 0 {
			outs[i].Printf("http://localhost:%d", s.Port)
		}
	}

	var wg sync.WaitGroup
	for i, s := range services {
		wg.Go(func() { runService(ctx, root, s, outs[i]) })
	}
	wg.Wait()
	return nil
}

// busyPorts lists the services whose port already accepts connections.
func busyPorts(services []Service) []string {
	var busy []string
	for _, s := range services {
		if s.Port == 0 {
			continue
		}
		for _, host := range []string{"127.0.0.1", "::1"} {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(s.Port)), 300*time.Millisecond)
			if err == nil {
				conn.Close()
				busy = append(busy, fmt.Sprintf("%s :%d", s.Name, s.Port))
				break
			}
		}
	}
	return busy
}
