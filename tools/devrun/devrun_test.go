package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoConfig(t *testing.T) {
	c, err := loadConfig("../../dev.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range c.Services {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "gateway,mcp,api,web" {
		t.Fatalf("services = %v", names)
	}
	if _, err := c.selectServices([]string{"api", "nope"}); err == nil {
		t.Fatal("unknown service accepted")
	}
}

func TestSnapshot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "svc", "main.go"), "package main")
	s := Service{Watch: []string{"svc"}, Ext: []string{"go", "yaml"}}
	base := snapshot(root, s.Watch, s.Ext)

	// Ignored: tests, other extensions, dependency and hidden directories.
	write(t, filepath.Join(root, "svc", "main_test.go"), "package main")
	write(t, filepath.Join(root, "svc", "notes.md"), "x")
	write(t, filepath.Join(root, "svc", "node_modules", "x.go"), "x")
	write(t, filepath.Join(root, "svc", ".cache", "x.go"), "x")
	if snapshot(root, s.Watch, s.Ext) != base {
		t.Fatal("ignored files changed the snapshot")
	}

	write(t, filepath.Join(root, "svc", "config", "app.yaml"), "a: 1")
	added := snapshot(root, s.Watch, s.Ext)
	if added == base {
		t.Fatal("new file not detected")
	}
	if err := os.Remove(filepath.Join(root, "svc", "config", "app.yaml")); err != nil {
		t.Fatal(err)
	}
	if snapshot(root, s.Watch, s.Ext) != base {
		t.Fatal("deleting the new file should restore the snapshot")
	}
}

func TestOutputPrefixesLines(t *testing.T) {
	var b strings.Builder
	o := &output{mu: &sync.Mutex{}, w: &b, prefix: "api | "}
	_, _ = o.Write([]byte("one\ntw"))
	_, _ = o.Write([]byte("o\n"))
	if b.String() != "api | one\napi | two\n" {
		t.Fatalf("got %q", b.String())
	}
}

// syncBuffer is a goroutine-safe log sink for runService.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, log *syncBuffer, want string, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(log.String(), want) >= count {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d× %q in:\n%s", count, want, log.String())
}

// A change rebuilds and restarts; a failed build keeps the old process; Ctrl+C
// (context cancel) stops everything, including child processes.
func TestRunServiceRestartsOnChange(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "version.txt")
	write(t, src, "1")
	// "build" fails when the source says "broken"; "run" prints the version and stays up.
	s := Service{
		Name:  "svc",
		Dir:   "svc",
		Build: `! grep -q broken version.txt && cp version.txt built.txt`,
		Run:   `echo "running $(cat built.txt)"; exec sleep 60`,
		Watch: []string{"svc"},
		Ext:   []string{"txt"},
	}
	log := &syncBuffer{}
	out := &output{mu: &sync.Mutex{}, w: log, prefix: ""}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runService(ctx, root, s, out)
		close(done)
	}()

	waitFor(t, log, "running 1", 1)
	time.Sleep(settleDelay) // modification times need to move
	write(t, src, "2")
	waitFor(t, log, "running 2", 1)

	write(t, src, "broken")
	waitFor(t, log, "build failed", 1)
	if strings.Contains(log.String(), "running broken") {
		t.Fatal("a failed build must not restart the service")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(stopTimeout + 2*time.Second):
		t.Fatal("runService did not stop")
	}
}
