package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/kovidgoyal/kitty/tools/cli"
	"github.com/kovidgoyal/kitty/tools/utils"

	"wm/bar"
	"wm/display"
	"wm/hypr"
	"wm/quit_session"
	"wm/screenshot"
	"wm/sway"
)

func main() {
	root := cli.NewRootCommand()
	root.ShortDescription = "A tool to ease integration with Wayland compositors"
	root.HelpText = "Serves as an entrypoint for various tools such as bar."
	root.Usage = "command [command options] [command args]"
	root.Run = func(cmd *cli.Command, args []string) (int, error) {
		cmd.ShowHelp()
		return 0, nil
	}

	root.AddSubCommand(&cli.Command{
		Name:             "bar",
		ShortDescription: "Top bar for desktop",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			bar.Main(args)
			return
		},
	})
	root.AddSubCommand(&cli.Command{
		Name:             "screenshot",
		ShortDescription: "Take a screenshot",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			screenshot.Main(args)
			return
		},
	})
	root.AddSubCommand(&cli.Command{
		Name:             "quit_session",
		ShortDescription: "Quit the current session",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			quit_session.Main(args)
			return
		},
	})
	display.AddEntryPoints(root.AddSubCommand(&cli.Command{
		Name:             "display",
		ShortDescription: "Control the monitors",
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			cmd.ShowHelp()
			return
		},
	}))
	root.AddSubCommand(&cli.Command{
		Name:             "togglestack",
		ShortDescription: "Toggle stacked layout in Hyprland since it doesnt have this functionality builtin",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			return hypr.ToggleStackMain(args)
		},
	})
	root.AddSubCommand(&cli.Command{
		Name:             "workspace",
		Usage:            " workspace_name",
		ShortDescription: "Change to the specified workspace",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if len(args) != 1 {
				cmd.ShowHelp()
				return 1, nil
			}
			switch {
			case hypr.IsHyprlandRunning():
				err = hypr.ChangeToWorkspace(args[0])
			case sway.IsSwayRunning():
				err = sway.ChangeToWorkspace(args[0])
			default:
				err = fmt.Errorf("No supported Wayland compositor is running")
			}
			return utils.IfElse(err == nil, 0, 1), err
		},
	})
	root.AddSubCommand(&cli.Command{
		Name:             "move-to-workspace",
		Usage:            " workspace_name",
		ShortDescription: "Move the active window to the specified workspace",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if len(args) != 1 {
				cmd.ShowHelp()
				return 1, nil
			}
			switch {
			case hypr.IsHyprlandRunning():
				err = hypr.MoveToWorkspace(args[0])
			default:
				err = fmt.Errorf("No supported Wayland compositor is running")
			}
			return utils.IfElse(err == nil, 0, 1), err
		},
	})

	root.AddSubCommand(&cli.Command{
		Name:             "super-tab",
		ShortDescription: "Switch between windows in a group if a group is active otherwise switch normally",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if len(args) != 0 {
				cmd.ShowHelp()
				return 1, nil
			}
			if err = hypr.SuperTab(); err != nil {
				rc = 1
			}
			return
		},
	})
	const BELOW_DEFAULT = 0
	const ABOVE_DEFAULT = 100
	root.AddSubCommand(&cli.Command{
		Name:             "charge-control",
		ShortDescription: "Set thresholds to control when the battery charges",
		Usage:            "start-charging-below stop-charging-above",
		HelpText:         fmt.Sprintf("Set percentage levels to control when the battery starts and stops charging. Default when unspecified is to not restrict charging, which means below is set to %d and above to %d. Specify the percentages as integers between 0 and 100.", BELOW_DEFAULT, ABOVE_DEFAULT),
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			below, above := BELOW_DEFAULT, ABOVE_DEFAULT
			if len(args) > 0 {
				if below, err = strconv.Atoi(args[0]); err != nil {
					return 1, err
				}
			}
			if len(args) > 1 {
				if above, err = strconv.Atoi(args[0]); err != nil {
					return 1, err
				}
			}
			const base = `/sys/class/power_supply`
			entries, err := os.ReadDir(base)
			if err != nil {
				return 1, err
			}
			for _, e := range entries {
				q := filepath.Join(base, e.Name())
				if _, ex := os.Stat(filepath.Join(q, "charge_control_start_threshold")); ex == nil {
					for name, val := range map[string]int{
						"charge_control_start_threshold": below, "charge_control_end_threshold": above} {
						if err = os.WriteFile(filepath.Join(q, name), []byte(strconv.Itoa(val)), 0644); err != nil {
							return 1, err
						}
					}

				}
			}
			return
		},
	})
	root.Exec(os.Args...)

}
