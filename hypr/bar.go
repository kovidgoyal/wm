package hypr

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/kovidgoyal/kitty/tools/utils"
)

var _ = fmt.Print
var repr = utils.Repr
var _ = repr

func handle_bar_event(line string, set_strings func(...string)) (err error) {
	which, payload, found := strings.Cut(line, ">>")
	if !found {
		return fmt.Errorf("Invalid event from hyprland: %s", line)
	}
	switch which {
	case "activewindow":
		_, title, found := strings.Cut(payload, ",")
		if found {
			set_strings("title:" + title)
		}
	case "workspace":
		set_strings("workspace:" + payload)
	case "focusedmon":
		_, name, found := strings.Cut(payload, ",")
		if found {
			set_strings("workspace:" + name)
		}
	case "bell":
		// See https://github.com/hyprwm/Hyprland/discussions/10428
		// Eventually use: https://specifications.freedesktop.org/sound-theme-spec/latest/sound_lookup.html
		// to avoid hardcoding sound file path.
		cmd := exec.Command("pw-play", "/usr/share/sounds/ocean/stereo/bell.oga")
		go func() {
			cmd.Run()
		}()
	}
	return
}

func bar_loop(conn *net.UnixConn, set_strings func(...string)) {
	reader := bufio.NewReader(conn)
	defer conn.Close()
	for {
		if line, err := reader.ReadString('\n'); err != nil {
			debugprintln("Failed to read for Hyprland events socket with error:", err)
			var conn *net.UnixConn
			if conn, err = GetEventsConnection(); err != nil {
				debugprintln("Failed to reconnect to Hyprland events socket with error:", err)
				return
			}
			go bar_loop(conn, set_strings)
			return
		} else {
			line = strings.TrimSpace(line)
			if err := handle_bar_event(line, set_strings); err != nil {
				debugprintln("Failed to handle hyprland event: %s with error: %s", line, err)
			}
		}
	}
}

func HyprBar(set_strings func(...string)) (err error) {
	var conn *net.UnixConn
	if conn, err = GetEventsConnection(); err != nil {
		return err
	}
	var activeworkspace Workspace
	var activewindow Window
	if err = make_requests(request{"activeworkspace", &activeworkspace}, request{"activewindow", &activewindow}); err != nil {
		conn.Close()
		return
	}
	set_strings("title:"+activewindow.Title, "workspace:"+activeworkspace.Name)
	go bar_loop(conn, set_strings)
	return
}
