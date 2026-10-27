package dashboard

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Recipe struct {
	Command string            `toml:"command"`
	Columns []string          `toml:"columns"`
	Keys    map[string]Action `toml:"keys"`
	Refresh int               `toml:"refresh"`
	Colors  map[string]any    `toml:"colors"`
	Headers *bool             `toml:"headers"`
	Labels  map[string]string `toml:"labels"`
	Fit     bool              `toml:"fit"`
	Where   map[string]any    `toml:"where"`
	Days    int               `toml:"days"`
	Date    string            `toml:"date"`
	End     string            `toml:"end"`
	Cache   any               `toml:"cache"`
}

type Cache struct {
	Name  string   `toml:"name"`
	TTL   string   `toml:"ttl"`
	Watch []string `toml:"watch"`
}

func (r Recipe) cache() (*Cache, error) {
	switch v := r.Cache.(type) {
	case nil:
		return nil, nil
	case string:
		return &Cache{TTL: v}, nil
	case map[string]any:
		b, err := toml.Marshal(v)
		if err != nil {
			return nil, err
		}
		var c Cache
		if err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&c); err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
		return &c, nil
	}
	return nil, fmt.Errorf("cache: want a duration like \"10m\" or { name = \"...\", ttl = \"10m\", watch = [...] }, got %v", r.Cache)
}

type Action struct {
	Run     []string `toml:"run"`
	Quit    bool     `toml:"quit"`
	Confirm string   `toml:"confirm"`
	Bust    []string `toml:"bust"`
}

func (a *Action) UnmarshalTOML(data []byte) error {
	value := append([]byte("v = "), data...)
	var list struct{ V []string }
	if toml.Unmarshal(value, &list) == nil {
		*a = Action{Run: list.V}
		return nil
	}
	var inline struct{ V Action }
	if toml.Unmarshal(value, &inline) == nil {
		*a = inline.V
		return nil
	}
	if toml.Unmarshal(data, a) == nil {
		return nil
	}
	return fmt.Errorf("want a list of arguments or { run = [...], quit = true, confirm = \"...\", bust = [...] }, got %s", bytes.TrimSpace(data))
}

type SectionConfig struct {
	Title string          `toml:"title"`
	Use   string          `toml:"recipe"`
	Stack []SectionConfig `toml:"stack"`
	Tabs  []SectionConfig `toml:"tabs"`
	Recipe
}

type PageConfig struct {
	Title    string            `toml:"title"`
	Sections [][]SectionConfig `toml:"sections"`
}

type Config struct {
	Import  []string          `toml:"import"`
	Recipes map[string]Recipe `toml:"recipe"`
	Pages   []PageConfig      `toml:"page"`
}

func DefaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hud", "hud.toml")
}

func LoadConfig(path string) (Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	return loadConfig(path, map[string]bool{})
}

func loadConfig(path string, seen map[string]bool) (Config, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Config{}, err
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return Config{}, err
	}
	if seen[abs] {
		return Config{}, nil
	}
	seen[abs] = true
	own, err := decodeConfig(path)
	if err != nil {
		return own, err
	}
	cfg := Config{Recipes: map[string]Recipe{}}
	origin := map[string]string{}
	add := func(file string, c Config) error {
		for name, r := range c.Recipes {
			if prev, ok := origin[name]; ok {
				return fmt.Errorf("recipe %q is defined in both %s and %s", name, prev, file)
			}
			origin[name] = file
			cfg.Recipes[name] = r
		}
		cfg.Pages = append(cfg.Pages, c.Pages...)
		return nil
	}
	for _, imp := range own.Import {
		file := expandHome(imp)
		if !filepath.IsAbs(file) {
			file = filepath.Join(filepath.Dir(abs), file)
		}
		sub, err := loadConfig(file, seen)
		if err != nil {
			return cfg, err
		}
		if err := add(file, sub); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := add(path, own); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func decodeConfig(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = toml.NewDecoder(bytes.NewReader(b)).EnableUnmarshalerInterface().DisallowUnknownFields().Decode(&cfg)
	if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
		return cfg, fmt.Errorf("%s: unknown fields:\n%s", path, strict.String())
	}
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) resolve(sc SectionConfig) (Recipe, error) {
	r, err := c.merge(sc)
	if err != nil {
		return r, err
	}
	if r.Days > 0 && (r.Date == "" || len(r.Columns) == 0) {
		return r, fmt.Errorf("section %q: days needs date and columns", sc.Title)
	}
	if err := checkCache(r); err != nil {
		return r, fmt.Errorf("section %q: %w", sc.Title, err)
	}
	for col, rule := range r.Colors {
		if err := checkColorRule(rule); err != nil {
			return r, fmt.Errorf("section %q: colors.%s: %w", sc.Title, col, err)
		}
	}
	return r, nil
}

func checkCache(r Recipe) error {
	c, err := r.cache()
	if err != nil {
		return err
	}
	if c != nil {
		if c.TTL != "" {
			if _, err := time.ParseDuration(c.TTL); err != nil {
				return fmt.Errorf("cache.ttl: %w", err)
			}
		}
		if !validCacheName(c.Name) {
			return fmt.Errorf("cache.name %q can't contain a path", c.Name)
		}
	}
	for name, action := range r.Keys {
		for _, b := range action.Bust {
			if b == "" || !validCacheName(b) {
				return fmt.Errorf("keys.%s.bust: %q isn't a cache name", name, b)
			}
		}
	}
	return nil
}

func validCacheName(name string) bool {
	return name == "" || (filepath.Base(name) == name && name != "." && name != "..")
}

func checkColorRule(rule any) error {
	switch rule := rule.(type) {
	case string:
		if _, _, ok := parseStyle(rule); !ok && !strings.Contains(rule, "{{") {
			return fmt.Errorf("unknown style %q", rule)
		}
	case map[string]any:
		for _, v := range rule {
			if err := checkColorRule(v); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("want a color or a table of value = color, got %v", rule)
	}
	return nil
}

func (c Config) merge(sc SectionConfig) (Recipe, error) {
	sc.Keys = splitKeys(sc.Keys)
	if sc.Use == "" {
		return sc.Recipe, nil
	}
	base, ok := c.Recipes[sc.Use]
	if !ok {
		return Recipe{}, fmt.Errorf("section %q: unknown recipe %q", sc.Title, sc.Use)
	}
	base.Command = cmp.Or(sc.Command, base.Command)
	base.Refresh = cmp.Or(sc.Refresh, base.Refresh)
	base.Days = cmp.Or(sc.Days, base.Days)
	base.Date = cmp.Or(sc.Date, base.Date)
	base.End = cmp.Or(sc.End, base.End)
	if sc.Columns != nil {
		base.Columns = sc.Columns
	}
	if sc.Colors != nil {
		base.Colors = sc.Colors
	}
	base.Fit = base.Fit || sc.Fit
	if sc.Headers != nil {
		base.Headers = sc.Headers
	}
	if sc.Labels != nil {
		base.Labels = sc.Labels
	}
	if sc.Where != nil {
		base.Where = sc.Where
	}
	if sc.Cache != nil {
		base.Cache = sc.Cache
	}
	base.Keys = splitKeys(base.Keys)
	maps.Copy(base.Keys, sc.Keys)
	return base, nil
}

func splitKeys(keys map[string]Action) map[string]Action {
	out := make(map[string]Action, len(keys))
	for names, action := range keys {
		for _, name := range strings.Fields(names) {
			out[name] = action
		}
	}
	return out
}
