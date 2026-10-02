package dashboard

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Recipe struct {
	Command string              `toml:"command"`
	Columns []string            `toml:"columns"`
	Enter   []string            `toml:"enter"`
	Keys    map[string][]string `toml:"keys"`
	Refresh int                 `toml:"refresh"`
	Colors  map[string]any      `toml:"colors"`
	Headers *bool               `toml:"headers"`
	Labels  map[string]string   `toml:"labels"`
	Fit     bool                `toml:"fit"`
	Where   map[string]any      `toml:"where"`
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
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) resolve(sc SectionConfig) (Recipe, error) {
	r, err := c.merge(sc)
	if err != nil {
		return r, err
	}
	for col, rule := range r.Colors {
		if err := checkColorRule(rule); err != nil {
			return r, fmt.Errorf("section %q: colors.%s: %w", sc.Title, col, err)
		}
	}
	return r, nil
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
	if sc.Use == "" {
		return sc.Recipe, nil
	}
	base, ok := c.Recipes[sc.Use]
	if !ok {
		return Recipe{}, fmt.Errorf("section %q: unknown recipe %q", sc.Title, sc.Use)
	}
	base.Command = cmp.Or(sc.Command, base.Command)
	base.Refresh = cmp.Or(sc.Refresh, base.Refresh)
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
	if sc.Enter != nil {
		base.Enter = sc.Enter
	}
	if sc.Keys != nil {
		base.Keys = maps.Clone(base.Keys)
		if base.Keys == nil {
			base.Keys = map[string][]string{}
		}
		maps.Copy(base.Keys, sc.Keys)
	}
	return base, nil
}
