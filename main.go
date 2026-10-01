package main

import (
	"context"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/joshmedeski/hud/dashboard"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "hud",
		Version: version,
		Short:   "A terminal dashboard built from command recipes",
		Long:    "hud renders pages of panes, each fed by a shell command. JSON arrays become tables you can act on; any other output is shown as text.",
		Args:    cobra.NoArgs,
		RunE:    run,
	}
	root.Flags().StringP("page", "p", "", "title of the page to open first")
	root.Flags().StringP("config", "C", "", "path to config file (default $XDG_CONFIG_HOME/hud/hud.toml)")

	if err := fang.Execute(context.Background(), root, fang.WithColorSchemeFunc(fang.AnsiColorScheme), fang.WithoutVersion()); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	cfg, err := dashboard.LoadConfig(path)
	if err != nil {
		return err
	}
	m, err := dashboard.New(cfg)
	if err != nil {
		return err
	}
	if title, _ := cmd.Flags().GetString("page"); title != "" {
		if m, err = m.OpenPage(title); err != nil {
			return err
		}
	}
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return err
	}

	action := result.(dashboard.Model).Action()
	if len(action) == 0 {
		return nil
	}
	c := exec.Command(action[0], action[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
