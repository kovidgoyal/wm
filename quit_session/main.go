package quit_session

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
	"wm/hypr"
	"wm/screenshot"
	"wm/sway"

	"github.com/kovidgoyal/kitty/tools/tty"
	"github.com/kovidgoyal/kitty/tools/tui/loop"
	"golang.org/x/sys/unix"
)

var _ = fmt.Print
var debugprintln = tty.DebugPrintln

const (
	LOGOUT   = "L"
	REBOOT   = "R"
	POWEROFF = "P"
)

func do_shutdown(action string, pids []int) {
	// ignore signals so that when parent kitty is killed we are not killed
	var err error
	signal.Ignore(unix.SIGHUP, unix.SIGTERM, unix.SIGINT)
	rdir := os.Getenv("XDG_RUNTIME_DIR")
	if rdir == "" {
		rdir = fmt.Sprintf("/run/user/%d", os.Geteuid())
	}
	shutdown_action_path := filepath.Join(rdir, "my-session-shutdown-action")
	payload := ""
	switch action {
	case LOGOUT:
		payload = "logout"
	case REBOOT:
		payload = "reboot"
	case POWEROFF:
		payload = "poweroff"
	}
	os.WriteFile(shutdown_action_path, []byte(payload), 0o600)
	for _, pid := range pids {
		if pid != os.Getpid() {
			unix.Kill(pid, unix.SIGTERM)
		}
	}
	time.Sleep(time.Millisecond * 500)
	// parent kitty is dead cant print anything
	switch {
	case hypr.IsHyprlandRunning():
		err = hypr.ExitHyprland()
	case sway.IsSwayRunning():
		err = sway.ExitSway()
	}
	if err != nil {
		os.Exit(1)
	}
}

func run_loop() {
	var pids []int
	var err error
	switch {
	case hypr.IsHyprlandRunning():
		pids = hypr.GetPIDsForGracefulShutdown()
	case sway.IsSwayRunning():
		pids = sway.GetPIDsForGracefulShutdown()
	default:
		debugprintln("No supported Wayland compositor is running")
		os.Exit(1)
	}

	lp, err := loop.New()
	if err != nil {
		debugprintln(err)
		os.Exit(1)
	}
	draw_screen := func() (err error) {
		lp.StartAtomicUpdate()
		defer lp.EndAtomicUpdate()
		lp.ClearScreen()
		s := "fg=green bold intense"
		lines := []string{
			fmt.Sprintf("\x00%sogout  %seboot  %soweroff", lp.SprintStyled(s, LOGOUT), lp.SprintStyled(s, REBOOT), lp.SprintStyled(s, POWEROFF)),
			"",
			fmt.Sprintf("\x00Press %s to abort", lp.SprintStyled("italic fg=red", "Esc")),
		}
		screenshot.Draw_lines_in_subframe(lp, "bg=black", lines...)
		return
	}
	action := ""
	lp.OnKeyEvent = func(ev *loop.KeyEvent) (err error) {
		if ev.MatchesPressOrRepeat("esc") {
			lp.Quit(0)
			return
		}
		switch strings.ToLower(ev.Text) {
		case "l":
			action = LOGOUT
		case "r":
			action = REBOOT
		case "p":
			action = POWEROFF
		}
		if action != "" {
			lp.Quit(0)
		}
		return
	}

	lp.OnInitialize = func() (string, error) {
		lp.SetCursorVisible(false)
		lp.AllowLineWrapping(false)
		if err := draw_screen(); err != nil {
			return "", err
		}
		return "", nil
	}
	lp.OnResize = func(loop.ScreenSize, loop.ScreenSize) error {
		return draw_screen()
	}

	err = lp.Run()
	if err != nil {
		debugprintln(err)
		os.Exit(1)

	}
	if action != "" {
		do_shutdown(action, pids)
	}

	os.Exit(lp.ExitCode())
}

func Main(args []string) {
	screenshot.Panel_main(args, "quit_session", run_loop)
}
