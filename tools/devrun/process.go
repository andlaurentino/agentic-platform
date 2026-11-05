package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const stopTimeout = 5 * time.Second

// output writes each line of a service's output with a colored name prefix.
type output struct {
	mu     *sync.Mutex // shared by all services, so lines never interleave
	w      io.Writer
	prefix string
	buf    []byte
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, p...)
	for {
		i := bytes.IndexByte(o.buf, '\n')
		if i < 0 {
			break
		}
		fmt.Fprintf(o.w, "%s%s\n", o.prefix, o.buf[:i])
		o.buf = o.buf[i+1:]
	}
	return len(p), nil
}

// Printf writes a devrun message for this service.
func (o *output) Printf(format string, args ...any) {
	fmt.Fprintf(o, "devrun: "+format+"\n", args...)
}

// process is a running service command. It runs in its own process group so
// stopping it also stops its children (npm → vite, shell → binary).
type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func command(root string, s Service, line string, out io.Writer) *exec.Cmd {
	cmd := exec.Command("sh", "-c", line)
	cmd.Dir = filepath.Join(root, s.Dir)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// runOnce runs a build or prepare step to completion, killing it if ctx ends.
func runOnce(ctx context.Context, root string, s Service, line string, out io.Writer) error {
	cmd := command(root, s, line, out)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return ctx.Err()
	}
}

func start(root string, s Service, out io.Writer) (*process, error) {
	p := &process{cmd: command(root, s, s.Run, out), done: make(chan struct{})}
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// stop interrupts the process group, then kills it if it has not exited in time.
func (p *process) stop() {
	if p == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT)
	select {
	case <-p.done:
	case <-time.After(stopTimeout):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}

// runService keeps one service running until ctx ends: prepare once, then
// build and (re)start on every change in its watched directories. A failed
// build keeps the previous process running.
func runService(ctx context.Context, root string, s Service, out *output) {
	if s.Prepare != "" {
		if err := runOnce(ctx, root, s, s.Prepare, out); err != nil {
			if ctx.Err() == nil {
				out.Printf("prepare failed: %v", err)
			}
			return
		}
	}
	var proc *process
	defer func() { proc.stop() }()
	last := snapshot(root, s.Watch, s.Ext)
	for first := true; ; first = false {
		built := true
		if s.Build != "" {
			if !first {
				out.Printf("change detected, rebuilding")
			}
			if err := runOnce(ctx, root, s, s.Build, out); err != nil {
				if ctx.Err() != nil {
					return
				}
				built = false
				out.Printf("build failed; fix it and save to retry (the previous version keeps running)")
			}
		}
		if built {
			if proc != nil {
				out.Printf("restarting")
				proc.stop()
			}
			var err error
			if proc, err = start(root, s, out); err != nil {
				out.Printf("start failed: %v", err)
				proc = nil
			}
		}

		var exited <-chan struct{}
		if proc != nil {
			exited = proc.done
		}
		if len(s.Watch) == 0 {
			select {
			case <-ctx.Done():
			case <-exited:
				out.Printf("exited: %v", exitStatus(proc.err))
			}
			return
		}
		next, changed := waitForChange(ctx, root, s, last, exited)
		if ctx.Err() != nil {
			return
		}
		if !changed { // the process exited by itself
			out.Printf("exited: %v; waiting for changes to restart", exitStatus(proc.err))
			proc = nil
			if next, changed = waitForChange(ctx, root, s, last, nil); !changed {
				return
			}
		}
		last = next
	}
}

func exitStatus(err error) string {
	if err == nil {
		return "status 0"
	}
	return err.Error()
}
