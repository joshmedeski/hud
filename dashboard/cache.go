package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type cache struct {
	name  string
	ttl   time.Duration
	watch []string
}

func newCache(r Recipe) *cache {
	c, _ := r.cache()
	if c == nil {
		return nil
	}
	name := c.Name
	if name == "" {
		sum := sha256.Sum256([]byte(r.Command))
		name = hex.EncodeToString(sum[:4])
	}
	ttl, _ := time.ParseDuration(c.TTL)
	watch := make([]string, len(c.Watch))
	for i, w := range c.Watch {
		watch[i] = expandHome(w)
	}
	return &cache{name: name, ttl: ttl, watch: watch}
}

func CacheDir() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "hud")
}

func cachePath(name string) string { return filepath.Join(CacheDir(), name) }

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	return path
}

func (c *cache) read() (out []byte, at time.Time, stale, ok bool) {
	if c == nil {
		return nil, at, false, false
	}
	info, err := os.Stat(cachePath(c.name))
	if err != nil {
		return nil, at, false, false
	}
	if out, err = os.ReadFile(cachePath(c.name)); err != nil {
		return nil, at, false, false
	}
	at = info.ModTime()
	stale = c.ttl > 0 && time.Since(at) > c.ttl
	for _, w := range c.watch {
		if wi, err := os.Stat(w); err == nil && wi.ModTime().After(at) {
			stale = true
		}
	}
	return out, at, stale, true
}

func (c *cache) write(out []byte) {
	if c == nil || os.MkdirAll(CacheDir(), 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(CacheDir(), c.name+".*")
	if err != nil {
		return
	}
	_, err = tmp.Write(out)
	if tmp.Close() != nil || err != nil {
		os.Remove(tmp.Name())
		return
	}
	os.Rename(tmp.Name(), cachePath(c.name))
}

func bust(names []string) {
	for _, name := range names {
		os.Remove(cachePath(name))
	}
}

func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
