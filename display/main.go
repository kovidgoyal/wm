package display

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"wm/hypr"
	"wm/sway"

	"github.com/kovidgoyal/kitty/tools/cli"
	"github.com/kovidgoyal/kitty/tools/utils"
)

var _ = fmt.Print

func change_power(action string) (err error) {
	switch {
	case sway.IsSwayRunning():
		err = sway.TogglePower(action, "*")
	case hypr.IsHyprlandRunning():
		err = hypr.TogglePower(action, "*")
	default:
		err = fmt.Errorf("No supported Wayland compositor is running")
	}
	return
}

func change_brightness(brighter bool) (err error) {
	cmd := exec.Command("brightnessctl", "-m", "i")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err = cmd.Run(); err != nil {
		return
	}
	for _, line := range utils.Splitlines(stdout.String()) {
		fields := strings.FieldsFunc(line, func(q rune) bool { return q == ',' })
		device_type := fields[1]
		if device_type == "backlight" {
			current_brightness, err := strconv.Atoi(fields[2])
			if err != nil {
				return err
			}
			max_brightness, err := strconv.Atoi(fields[4])
			if err != nil {
				return err
			}
			mult := utils.IfElse(brighter, 1, -1)
			delta := max(1, max_brightness/10)
			new_brightness := max(0, min(current_brightness+mult*delta, max_brightness))
			device_name := fields[0]
			cmd := exec.Command("brightnessctl", "-d", device_name, "set", strconv.Itoa(new_brightness))
			if err = cmd.Run(); err != nil {
				return err
			}
		}
	}
	return
}

func AddEntryPoints(display_cmd *cli.Command) {
	display_cmd.AddSubCommand(&cli.Command{
		Name:             "brighter",
		ShortDescription: "Make display brighter",
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if err = change_brightness(true); err != nil {
				rc = 1
			}
			return
		},
	})
	display_cmd.AddSubCommand(&cli.Command{
		Name:             "dimmer",
		ShortDescription: "Make display dimmer",
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if err = change_brightness(false); err != nil {
				rc = 1
			}
			return
		},
	})
	power := display_cmd.AddSubCommand(&cli.Command{
		Name:             "power",
		ShortDescription: "Manage display power state",
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			cmd.ShowHelp()
			return
		},
	})
	power.AddSubCommand(&cli.Command{
		Name:             "toggle",
		ShortDescription: "Toggle displays on/off",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if err = change_power("toggle"); err != nil {
				rc = 1
			}
			return
		},
	})
	power.AddSubCommand(&cli.Command{
		Name:             "on",
		ShortDescription: "Turn display on",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if err = change_power("on"); err != nil {
				rc = 1
			}
			return
		},
	})
	power.AddSubCommand(&cli.Command{
		Name:             "off",
		ShortDescription: "Turn display off",
		OnlyArgsAllowed:  true,
		Run: func(cmd *cli.Command, args []string) (rc int, err error) {
			if err = change_power("off"); err != nil {
				rc = 1
			}
			return
		},
	})
}
