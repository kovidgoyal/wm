package hypr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"wm/common"

	"github.com/kovidgoyal/kitty/tools/tty"
	"github.com/kovidgoyal/kitty/tools/utils"

	"golang.org/x/sys/unix"
)

var _ = fmt.Print
var debugprintln = tty.DebugPrintln

const HIS = "HYPRLAND_INSTANCE_SIGNATURE"

var RuntimeDir = sync.OnceValue(func() string {
	his := os.Getenv(HIS)
	if his == "" {
		return ""
	}
	rdir := os.Getenv("XDG_RUNTIME_DIR")
	if rdir == "" {
		rdir = fmt.Sprintf("/run/user/%d", os.Geteuid())
	}
	rdir = filepath.Join(rdir, "hypr", his)
	if unix.Access(rdir, unix.X_OK|unix.R_OK) != nil {
		rdir = ""
	}
	return rdir
})

func get_conn(which string) (conn *net.UnixConn, err error) {
	rdir := RuntimeDir()
	if rdir == "" {
		return nil, fmt.Errorf("The Hyprland compositor does not seem to be running, could not find its socket")
	}
	sockaddr := filepath.Join(rdir, which)
	var addr *net.UnixAddr
	if addr, err = net.ResolveUnixAddr("unix", sockaddr); err != nil {
		return
	}
	conn, err = net.DialUnix("unix", nil, addr)
	return
}

func GetControlConnection() (conn *net.UnixConn, err error) {
	return get_conn(".socket.sock")
}

func GetEventsConnection() (conn *net.UnixConn, err error) {
	return get_conn(".socket2.sock")
}

// IPC data types {{{
type Window struct {
	Address           string   `json:"address"`
	Class             string   `json:"class"`
	Title             string   `json:"Title"`
	Initial_class     string   `json:"initialClass"`
	Initial_title     string   `json:"initialTitle"`
	Mapped            bool     `json:"mapped"`
	Hidden            bool     `json:"hidden"`
	Floating          bool     `json:"floating"`
	Pseudo            bool     `json:"psueudo"`
	Xwayland          bool     `json:"xwayland"`
	Pinned            bool     `json:"pinned"`
	Fullscreen        int      `json:"fullscreen"`
	Fullscreen_client int      `json:"fullscreenClient"`
	Grouped           []string `json:"grouped"`
	Monitor           int      `json:"monitor"`
	Pid               int      `json:"pid"`
	At                [2]int   `json:"at"`
	Size              [2]int   `json:"size"`
	Workspace         struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	Tags             []string `json:"tags"`
	Swallowing       string   `json:"swallowing"`
	Focus_history_id int      `json:"focusHistoryID"`
	Inhibiting_idle  bool     `json:"inhibitingIdle"`
	XDG_tag          string   `json:"xdgTag"`
	XDG_description  string   `json:"xdgDescription"`
}

func (c Window) String() string {
	s, _ := json.MarshalIndent(&c, "", "  ")
	return string(s)
}

type Monitor struct {
	Id               int     `json:"id"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Make             string  `json:"make"`
	Model            string  `json:"model"`
	Serial           string  `json:"serial"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Refresh_rate     float64 `json:"refreshRate"`
	X                int     `json:"x"`
	Y                int     `json:"y"`
	Active_workspace struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
	} `json:"activeWorkspace"`
	Special_workspace struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
	} `json:"specialWorkspace"`
	Reserved          []int    `json:"reserved"`
	Scale             float64  `json:"scale"`
	Transform         int      `json:"transform"`
	Focused           bool     `json:"focused"`
	DPMS_status       bool     `json:"dpmsStatus"`
	Vrr               bool     `json:"vrr"`
	Solitary          string   `json:"solitary"`
	Actively_tearing  bool     `json:"activelyTearing"`
	Direct_scanout_to string   `json:"directScanoutTo"`
	Disabled          bool     `json:"disabled"`
	Current_format    string   `json:"currentFormat"`
	Mirror_of         string   `json:"mirrorOf"`
	Available_modes   []string `json:"availableModes"`
}

func (c Monitor) String() string {
	s, _ := json.MarshalIndent(&c, "", "  ")
	return string(s)
}

type Workspace struct {
	Id                int    `json:"id"`
	Name              string `json:"name"`
	Monitor           string `json:"monitor"`
	Monitor_id        int    `json:"monitorID"`
	Windows           int    `json:"windows"`
	Has_fullscreen    bool   `json:"hasfullscreen"`
	Last_window       string `json:"lastwindow"`
	Last_window_title string `json:"lastwindowtitle"`
	Is_Persistent     bool   `json:"ispersistent"`
}

func (c Workspace) String() string {
	s, _ := json.MarshalIndent(&c, "", "  ")
	return string(s)
}

// }}}

type request struct {
	cmd      string
	response any
}

func send_commands(commands ...string) (responses []string, err error) {
	var conn *net.UnixConn
	if conn, err = GetControlConnection(); err != nil {
		return
	}
	defer conn.Close()
	q := strings.Builder{}
	q.WriteString("[[BATCH]]")
	for _, r := range commands {
		q.WriteString("j/")
		q.WriteString(r)
		q.WriteString(";")
	}
	if _, err = conn.Write(utils.UnsafeStringToBytes(q.String())); err != nil {
		return
	}
	var data []byte
	if data, err = io.ReadAll(conn); err != nil {
		return
	}
	for _, c := range commands {
		pos := bytes.Index(data, []byte{'\n', '\n', '\n'})
		chunk := utils.IfElse(pos < 0, data, data[:pos+3])
		q := strings.TrimSpace(string(chunk))
		responses = append(responses, q)
		if q != "ok" {
			err = fmt.Errorf("The command: %s returned an error response: %s", c, q)
		}
		data = data[len(chunk):]
	}
	return

}

func make_requests(requests ...request) (err error) {
	var conn *net.UnixConn
	if conn, err = GetControlConnection(); err != nil {
		return
	}
	defer conn.Close()
	q := strings.Builder{}
	q.WriteString("[[BATCH]]")
	for _, r := range requests {
		q.WriteString("j/")
		q.WriteString(r.cmd)
		q.WriteString(";")
	}
	if _, err = conn.Write(utils.UnsafeStringToBytes(q.String())); err != nil {
		return
	}
	var data []byte
	if data, err = io.ReadAll(conn); err != nil {
		return
	}
	for _, r := range requests {
		pos := bytes.Index(data, []byte{'\n', '\n', '\n'})
		chunk := utils.IfElse(pos < 0, data, data[:pos+3])
		if err = json.Unmarshal(chunk, r.response); err != nil {
			return
		}
		data = data[len(chunk):]
	}
	return
}

func (self Window) Direction_to(dest Window) string {
	switch {
	case self.At[0] == dest.At[0]:
		return utils.IfElse(self.At[1] < dest.At[1], "d", "u")
	case self.At[0] < dest.At[0]:
		return "r"
	default:
		return "l"
	}
}

func GetWindowRegions() (regions []common.WindowRegion, err error) {
	var workspace Workspace
	var windows []Window
	if err = make_requests(request{cmd: "activeworkspace", response: &workspace}, request{cmd: "clients", response: &windows}); err != nil {
		return
	}
	seen := make(map[[4]int]bool)
	for _, w := range windows {
		if w.Workspace.Id == workspace.Id {
			region := [4]int{w.At[0], w.At[1], w.Size[0], w.Size[1]}
			if !seen[region] {
				seen[region] = true
				regions = append(regions, common.WindowRegion{X: region[0], Y: region[1], Width: region[2], Height: region[3], Label: w.Title})
			}
		}
	}
	return
}

func GetPIDsForGracefulShutdown() []int {
	var windows []Window
	if err := make_requests(request{cmd: "clients", response: &windows}); err != nil {
		return nil
	}
	ans := make([]int, 0, len(windows))
	for _, w := range windows {
		if w.Pid > 0 && w.Class != "" {
			ans = append(ans, w.Pid)
		}
	}
	return ans
}

func ExitHyprland() (err error) {
	_, err = send_commands("eval hl.dispatch(hl.dsp.exit())")
	return err
}

func focus_window(addr string) string {
	return fmt.Sprintf(`eval hl.dispatch(hl.dsp.focus({ window = "address:%s" }))`, addr)
}

func move_active_window_out_of_group() string {
	return `eval hl.dispatch(hl.dsp.window.move({ out_of_group = "l" }))`
}

func move_window_in_direction(addr, direction string, group_aware bool) string {
	return fmt.Sprintf(`eval hl.dispatch(hl.dsp.window.move({ window = "address:%s", direction = "%s", group_aware = %v }))`, addr, direction, group_aware)
}

func make_window_into_group(addr string) string {
	return fmt.Sprintf(`eval hl.dispatch(hl.dsp.group.toggle({ window = "address:%s" }))`, addr)
}

func window_is_grouped(w Window) bool {
	ans := len(w.Grouped)
	for _, x := range w.Grouped {
		if x == w.Address {
			ans--
		}
	}
	return ans > 0
}

func toggle_stack() (err error) {
	var workspace Workspace
	var clients []Window
	var active_window Window
	if err = make_requests(request{cmd: "activeworkspace", response: &workspace}, request{cmd: "clients", response: &clients}, request{cmd: "activewindow", response: &active_window}); err != nil {
		return
	}
	window_is_moveable := func(c Window) bool { return !c.Floating && c.Workspace.Id == workspace.Id }
	clients = utils.Filter(clients, window_is_moveable)
	if len(clients) == 0 {
		return
	}
	is_grouped := slices.ContainsFunc(clients, window_is_grouped)
	if is_grouped {
		for _, c := range clients {
			send_commands(focus_window(c.Address), move_active_window_out_of_group())
		}
		// Make active window the master
		_, err = send_commands(focus_window(active_window.Address), move_window_in_direction(active_window.Address, "l", false))
		return
	}

	if len(clients) > 0 { // group all windows
		q := clients[0]
		for _, x := range clients {
			if x.Address == active_window.Address {
				q = x
				break
			}
		}
		if len(q.Grouped) == 0 {
			if _, err = send_commands(focus_window(q.Address), make_window_into_group(q.Address)); err != nil {
				return
			}
		}
		addresses_to_move := utils.NewSet[string](len(clients))
		for _, c := range clients {
			if c.Address != q.Address {
				addresses_to_move.Add(c.Address)
			}
		}
		for addresses_to_move.Len() > 0 {
			addr := addresses_to_move.Any()
			addresses_to_move.Discard(addr)
			var nclients []Window
			if err = make_requests(request{"clients", &nclients}); err != nil {
				return
			}
			nclients = utils.Filter(nclients, window_is_moveable)
			var dest Window
			found := false
			for _, x := range nclients {
				if x.Address == q.Address {
					found = true
					dest = x
					break
				}
			}
			if found {
				for _, x := range nclients {
					if x.Address == q.Address {
						continue
					}
					direction := x.Direction_to(dest)
					if _, err = send_commands(
						move_window_in_direction(x.Address, direction, true),
					); err != nil {
						return
					}
					break
				}
			}
		}
		if _, err = send_commands(focus_window(active_window.Address)); err != nil {
			return
		}
	}
	return
}

func ToggleStackMain(args []string) (rc int, err error) {
	// Simplify if https://github.com/hyprwm/Hyprland/discussions/10464 is implemented
	err = toggle_stack()
	return utils.IfElse(err == nil, 0, 1), err
}

func TogglePower(action, output_name_glob string) (err error) {
	var monitors []Monitor
	if err = make_requests(request{"monitors all", &monitors}); err != nil {
		return
	}
	commands := []string{}
	for _, m := range monitors {
		if match, err := filepath.Match(output_name_glob, m.Name); err != nil {
			return err
		} else if match {
			commands = append(commands, fmt.Sprintf(`eval hl.dispatch(hl.dsp.dpms({ action = "%s", monitor = "%s" }))`, action, m.Name))
		}
	}
	if len(commands) > 0 {
		// issue the actual dpms command after a second so that any key release events dont re-awaken the monitors
		// this should really be fixed in hyprland by having it not wakeup on release events.
		time.Sleep(time.Second)
		_, err = send_commands(commands...)
	}
	return
}

func ChangeToWorkspace(name string) (err error) {
	_, err = send_commands(switch_to_worksapce(name))
	return
}

func movetoworkspacesilent(name string) string {
	return fmt.Sprintf(`eval hl.dispatch(hl.dsp.window.move({ workspace = "name:%s", silent = true }))`, name)
}

func switch_to_worksapce(name string) string {
	return fmt.Sprintf(`eval hl.dispatch(hl.dsp.focus({ workspace = "name:%s" }))`, name)
}

// move the window managing the window stacks in source and description workspaces
func move_to_workspace(active_workspace Workspace, active_window Window, target_workspace Workspace, windows []Window) (err error) {
	if active_workspace.Id == target_workspace.Id {
		return
	}
	cmds := []string{}
	active_window_was_grouped := len(active_window.Grouped) > 0
	target_workspace_is_stacked := false
	for _, w := range windows {
		if w.Workspace.Id == target_workspace.Id && len(w.Grouped) > 0 {
			target_workspace_is_stacked = true
			break
		}
	}
	if active_window_was_grouped {
		cmds = append(cmds, make_window_into_group(active_window.Address))
	}
	cmds = append(cmds, movetoworkspacesilent(target_workspace.Name))
	if target_workspace_is_stacked {
		cmds = append(cmds,
			switch_to_worksapce(target_workspace.Name),
			focus_window(active_window.Address),
			move_window_in_direction(active_window.Address, "l", true),
			switch_to_worksapce(active_workspace.Name),
		)
	} else if target_workspace.Windows == 0 {
		// single window in target workspace so put it in stack layout
		cmds = append(cmds,
			switch_to_worksapce(target_workspace.Name),
			focus_window(active_window.Address),
			make_window_into_group(active_window.Address),
			switch_to_worksapce(active_workspace.Name),
		)
	}
	if _, err = send_commands(cmds...); err != nil {
		return
	}
	if active_window_was_grouped {
		// regroup remaining windows after we have moved out the active one
		err = toggle_stack()
	}

	return
}

func MoveToWorkspace(name string) (err error) {
	var workspaces []Workspace
	var active_workspace Workspace
	var active_window Window
	var windows []Window
	if err = make_requests(
		request{"workspaces", &workspaces}, request{"activeworkspace", &active_workspace}, request{"activewindow", &active_window},
		request{"clients", &windows},
	); err != nil {
		return
	}
	for _, w := range workspaces {
		if w.Name == name {
			return move_to_workspace(active_workspace, active_window, w, windows)
		}
	}
	// empty workspace, just move unconditionally
	cmds := []string{}
	active_window_was_grouped := len(active_window.Grouped) > 0
	if active_window_was_grouped {
		cmds = append(cmds, make_window_into_group(active_window.Address))
	}
	cmds = append(cmds, movetoworkspacesilent(name))
	if _, err = send_commands(cmds...); err != nil {
		return
	}
	if active_window_was_grouped {
		// regroup remaining windows after we have moved out the active one
		err = toggle_stack()
	}
	return
}

func SuperTab() (err error) {
	var window Window
	if err = make_requests(request{"activewindow", &window}); err != nil {
		return
	}
	cmd := utils.IfElse(len(window.Grouped) > 1, "eval hl.dispatch(hl.dsp.group.next())", "eval hl.dispatch(hl.dsp.window.cycle_next())")
	_, err = send_commands(cmd)
	return
}

func IsHyprlandRunning() bool {
	return RuntimeDir() != ""
}
