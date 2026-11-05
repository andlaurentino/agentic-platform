package main

import (
	"context"
	"hash/fnv"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Polling keeps devrun free of platform-specific file notification APIs; the
// watched trees are small enough to scan twice a second.
const (
	pollInterval = 500 * time.Millisecond
	settleDelay  = 300 * time.Millisecond // editors often write several files at once
)

var skipDirs = []string{"node_modules", "tmp", "dist", "testdata", "coverage"}

// snapshot fingerprints the watched files (path, size and modification time),
// so edits, new files and deletions all change it.
func snapshot(root string, dirs, exts []string) uint64 {
	h := fnv.New64a()
	for _, d := range dirs {
		_ = filepath.WalkDir(filepath.Join(root, d), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := e.Name()
			if e.IsDir() {
				if path != filepath.Join(root, d) && (strings.HasPrefix(name, ".") || slices.Contains(skipDirs, name)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !watched(name, exts) {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			h.Write([]byte(path + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n"))
			return nil
		})
	}
	return h.Sum64()
}

func watched(name string, exts []string) bool {
	if strings.HasSuffix(name, "_test.go") {
		return false
	}
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	return ext != "" && slices.Contains(exts, ext)
}

// waitForChange blocks until the snapshot differs from last and stays the
// same for settleDelay. It returns the new snapshot, or false when ctx ends
// or exited fires first.
func waitForChange(ctx context.Context, root string, s Service, last uint64, exited <-chan struct{}) (uint64, bool) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return last, false
		case <-exited:
			return last, false
		case <-t.C:
		}
		next := snapshot(root, s.Watch, s.Ext)
		if next == last {
			continue
		}
		for { // wait until writes settle
			select {
			case <-ctx.Done():
				return last, false
			case <-time.After(settleDelay):
			}
			again := snapshot(root, s.Watch, s.Ext)
			if again == next {
				return next, true
			}
			next = again
		}
	}
}
