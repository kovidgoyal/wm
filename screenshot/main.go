package screenshot

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"wm/common"
	"wm/hypr"
	"wm/sway"

	"github.com/kovidgoyal/kitty/kittens/clipboard"
	"github.com/kovidgoyal/kitty/tools/tty"
	"github.com/kovidgoyal/kitty/tools/tui/loop"
	"github.com/kovidgoyal/kitty/tools/utils"
	"github.com/kovidgoyal/kitty/tools/wcswidth"
	"golang.org/x/sys/unix"
)

const FILENAME = "/t/screenshot.png"

var instance_group = ""
var panel_cmdline = []string{
	"kitten", "panel", `--override=font_size=12`, `--override=background=#444`,
	"--override=background_opacity=0.4", "--override=confirm_os_window_close=0", "--override=close_on_child_death=y",
	"--override=clipboard_control=write-clipboard", "--override=clear_all_shortcuts=y",
	"-o=color0=#222", "-o=color7=#b8bcb9", "-o=color1=#be3e48", "-o=color2=#869a3a", "-o=color3=#c4a535",
	"--layer=overlay", "--edge=center", "--focus-policy=exclusive",
	"--toggle-visibility", "--single-instance",
}

var _ = fmt.Print
var debugprintln = tty.DebugPrintln

var encode_clipboard_chunk = clipboard.Encode_bytes

func get_instance_group(which string) string {
	switch {
	case hypr.IsHyprlandRunning():
		return which + "-" + hypr.RuntimeDir()
	case sway.IsSwayRunning():
		return which + "-" + sway.SocketAddr()
	default:
		fmt.Fprintln(os.Stderr, "No supported Wayland compositor is running")
		os.Exit(1)
	}
	return which
}

func Draw_lines_in_subframe(lp *loop.Loop, bg_style string, lines ...string) {
	sz, _ := lp.ScreenSize()
	screen_width := int(sz.WidthCells)
	screen_height := int(sz.HeightCells)
	width, height := 1, len(lines)+2
	for _, line := range lines {
		width = max(wcswidth.Stringwidth(line)+4, width)
	}
	height = min(screen_height, height)
	width = min(screen_width, width)
	x := max(0, screen_width-width) / 2
	y := max(0, screen_height-height) / 2
	for i, line := range lines {
		lp.MoveCursorTo(x+2, y+i+2)
		if line != "" {
			if line[0] == 0 {
				lwidth := wcswidth.Stringwidth(line)
				extra := width - lwidth
				if extra > 1 {
					lp.MoveCursorHorizontally(extra/2 - 1)
				}
			}
			lp.QueueWriteString(line)
		}
	}
	for i := range height {
		lp.MoveCursorTo(x+1, y+i+1)
		switch i {
		case 0:
			lp.QueueWriteString("╭")
			lp.QueueWriteString(strings.Repeat("─", width-2))
			lp.QueueWriteString("╮")
		case height - 1:
			lp.QueueWriteString("╰")
			lp.QueueWriteString(strings.Repeat("─", width-2))
			lp.QueueWriteString("╯")
		default:
			lp.QueueWriteString("│")
			lp.MoveCursorHorizontally(width - 2)
			lp.QueueWriteString("│")
		}
		lp.MoveCursorVertically(1)
	}
	lp.StyleRectangle(bg_style, x, y, x+width-1, y+height-1)
}

func copy_image_to_clipboard(lp *loop.Loop) {
	data, err := os.ReadFile(FILENAME)
	if err != nil {
		debugprintln("Failed to read from %s with error: %s", FILENAME, err)
		return
	}
	if len(data) < 1 {
		return
	}
	lp.QueueWriteString(encode_clipboard_chunk(map[string]string{"type": "write"}, nil))
	m := map[string]string{"type": "wdata", "mime": "image/png"}
	for len(data) > 0 {
		chunk := data[:min(len(data), 4096)]
		data = data[min(len(data), len(chunk)):]
		lp.QueueWriteString(encode_clipboard_chunk(m, chunk))
	}
	lp.QueueWriteString(encode_clipboard_chunk(map[string]string{"type": "wdata"}, nil))
}

func format_regions_for_slurp(regions []common.WindowRegion) string {
	ans := strings.Builder{}
	for _, r := range regions {
		ans.WriteString(fmt.Sprintf("%d,%d %dx%d %s\n", r.X, r.Y, r.Width, r.Height, strings.ReplaceAll(r.Label, "\n", " ")))
	}
	return ans.String()
}

func get_window_regions() (ans []common.WindowRegion, err error) {
	switch {
	case hypr.IsHyprlandRunning():
		ans, err = hypr.GetWindowRegions()
	case sway.IsSwayRunning():
		ans, err = sway.GetWindowRegions()
	default:
		err = fmt.Errorf("No supported Wayland compositor detected")
	}
	return
}

func run_loop() {
	lp, err := loop.New()
	if err != nil {
		debugprintln(err)
		os.Exit(1)
	}
	const (
		choosing_action = iota
		running_cmd
		showing_failure
	)
	state := choosing_action
	failed_cmd_msg := []string{}
	hidden := false
	lock := sync.Mutex{}
	failed_cmd_message_to_draw := []string{}

	get_failed_command_message := func() []string {
		lock.Lock()
		defer lock.Unlock()
		ans := failed_cmd_msg
		failed_cmd_msg = nil
		return ans
	}

	set_failed_command_message := func(script string, buf *bytes.Buffer, err error) {
		lock.Lock()
		defer lock.Unlock()
		if ee, ok := err.(*exec.ExitError); ok && buf != nil {
			failed_cmd_msg = utils.Splitlines(buf.String())
		} else {
			failed_cmd_msg = []string{fmt.Sprintf("Running %s failed with error: %s", script, ee)}
		}
	}

	hide_window := func() {
		if !hidden {
			hidden = true
			cmd := exec.Command(utils.Which(panel_cmdline[0]), panel_cmdline[1:]...)
			if err = cmd.Run(); err != nil {
				debugprintln("Failed to hide window with error:", err)
			}
		}
	}
	show_window := func() {
		if hidden {
			hidden = false
			cmd := exec.Command(utils.Which(panel_cmdline[0]), panel_cmdline[1:]...)
			if err = cmd.Run(); err != nil {
				debugprintln("Failed to show window with error:", err)
			}
		}
	}

	draw_choosing_action := func() (ans []string) {
		s := "fg=green bold intense"
		text := fmt.Sprintf("\x00%segion  %sindow  %sesktop", lp.SprintStyled(s, "R"), lp.SprintStyled(s, "W"), lp.SprintStyled(s, "D"))
		a := func(x string) { ans = append(ans, x) }
		a(text)
		a("")
		a(fmt.Sprintf("Press %s to abort", lp.SprintStyled("italic fg=red", "Esc")))
		a("")
		a("Screenshot will be:")
		a(fmt.Sprintf("  copied to %s", lp.SprintStyled("fg=yellow", "clipboard")))
		a(fmt.Sprintf("  saved to %s", lp.SprintStyled("fg=yellow", FILENAME)))
		return
	}

	draw_screen := func() (err error) {
		lp.StartAtomicUpdate()
		defer lp.EndAtomicUpdate()
		lp.ClearScreen()
		lines := []string{}
		switch state {
		case choosing_action:
			lines = draw_choosing_action()
		case running_cmd:
			lines = []string{"Running command, please wait..."}
		default:
			lines = append(lines, failed_cmd_message_to_draw...)
			lines = append(lines, "")
			lines = append(lines, "Press Esc to quit")
		}
		Draw_lines_in_subframe(lp, "bg=black", lines...)
		return
	}
	lp.OnWakeup = func() error {
		failed_cmd_message_to_draw = get_failed_command_message()
		state = utils.IfElse(len(failed_cmd_message_to_draw) == 0, choosing_action, showing_failure)
		if state == showing_failure {
			show_window()
			return draw_screen()
		}
		copy_image_to_clipboard(lp)
		return nil
	}
	lp.OnKeyEvent = func(ev *loop.KeyEvent) (err error) {
		if ev.MatchesPressOrRepeat("esc") {
			if state == showing_failure {
				state = choosing_action
				return draw_screen()
			}
			state = choosing_action
			hide_window()
			return
		}
		var script, input string
		switch strings.ToLower(ev.Text) {
		case "w":
			script = `slurp -r -d -f '%x,%y %wx%h %l\n' | grim -g -`
			if regions, err := get_window_regions(); err != nil {
				set_failed_command_message("getting_window_regions", nil, err)
				state = showing_failure
				draw_screen()
				return err
			} else {
				input = format_regions_for_slurp(regions)
			}
		case "r":
			script = `slurp -d | grim -g -`
		case "d":
			script = `grim`
		}
		if script != "" {
			script += fmt.Sprintf(` "%s"`, FILENAME)
			state = running_cmd
			hide_window()
			go func() {
				cmd := exec.Command("sh", "-c", script)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if input != "" {
					cmd.Stdin = bytes.NewReader([]byte(input))
				}
				var err error
				if err = cmd.Run(); err != nil && strings.Index(stderr.String(), "selection cancelled") < 0 {
					set_failed_command_message(script, &stderr, err)
				}
				lp.WakeupMainThread()
			}()
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
	lp.OnFocusChange = func(focused bool) error {
		hidden = !focused
		if focused {
			switch state {
			case choosing_action:
			case running_cmd:
				hide_window()
			case showing_failure:
			}
		} else {
			if state == showing_failure {
				state = choosing_action
			}
		}
		return draw_screen()
	}
	err = lp.Run()
	if err != nil {
		debugprintln(err)
		os.Exit(1)

	}
	os.Exit(lp.ExitCode())
}

func launch_panel(which string) {
	self_exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to get path to self executable: %w", err)
		os.Exit(1)
	}
	ig := get_instance_group(which)
	panel_cmdline = append(panel_cmdline, "--instance-group", ig, self_exe, which, "inner", ig)
	unix.Exec(utils.Which(panel_cmdline[0]), panel_cmdline, os.Environ())
}

func Panel_main(args []string, which string, run_loop func()) {
	if len(args) == 0 {
		launch_panel(which)
		return
	}
	if args[0] != "inner" {
		fmt.Fprintln(os.Stderr, args[0], "is not a valid argument")
		os.Exit(1)
	}
	panel_cmdline = append(panel_cmdline, "--instance-group", args[1])
	run_loop()
}

func Main(args []string) {
	Panel_main(args, "screenshot", run_loop)
}
