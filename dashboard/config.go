package dashboard

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Recipe struct {
	Command string              `toml:"command"`
	Columns []string            `toml:"columns"`
	Enter   []string            `toml:"enter"`
	Keys    map[string][]string `toml:"keys"`
	Refresh int                 `toml:"refresh"`
}

type SectionConfig struct {
	Title string `toml:"title"`
	Use   string `toml:"recipe"`
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

var builtinRecipes = map[string]Recipe{
	"sesh": {
		Command: "sesh list --json",
		Columns: []string{"Icon", "Name", "Path"},
		Enter:   []string{"sesh", "connect", "{{.Name}}"},
		Keys:    map[string][]string{"ctrl+d": {"tmux", "kill-session", "-t", "{{.Name}}"}},
	},
}

var defaultPages = []PageConfig{{
	Title:    "Dashboard",
	Sections: [][]SectionConfig{{{Title: "Sessions", Use: "sesh"}}},
}}

func DefaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hud", "hud.toml")
}

func LoadConfig(path string) (Config, error) {
	explicit := path != ""
	if !explicit {
		path = DefaultConfigPath()
	}

	var cfg Config
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !explicit:
	case err != nil:
		return cfg, err
	default:
		if err := toml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	}

	recipes := maps.Clone(builtinRecipes)
	maps.Copy(recipes, cfg.Recipes)
	cfg.Recipes = recipes
	if len(cfg.Pages) == 0 {
		cfg.Pages = defaultPages
	}
	return cfg, nil
}

func (c Config) resolve(sc SectionConfig) (Recipe, error) {
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
