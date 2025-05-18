package sway

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/kovidgoyal/kitty/tools/tty"
	"github.com/kovidgoyal/kitty/tools/utils"
)

var _ = fmt.Print
var debugprintln = tty.DebugPrintln

// See man 7 sway-ipc
const magic = "i3-ipc"
const (
	RUN_COMMAND     = 0
	GET_WORKSPACES  = 1
	SUBSCRIBE       = 2
	GET_OUTPUTS     = 3
	GET_TREE        = 4
	EVENT_WORKSPACE = 0x80000000
	EVENT_WINDOW    = 0x80000003
)

func SocketAddr() string {
	return os.Getenv("SWAYSOCK")
}

func IsSwayRunning() bool {
	return SocketAddr() != ""
}

func connect_to_sway() (conn *net.UnixConn, err error) {
	sockaddr := SocketAddr()
	if sockaddr == "" {
		return nil, os.ErrNotExist
	}
	var addr *net.UnixAddr
	if addr, err = net.ResolveUnixAddr("unix", sockaddr); err != nil {
		return
	}
	if conn, err = net.DialUnix("unix", nil, addr); err != nil {
		return
	}
	return
}

func swaymsg(conn *net.UnixConn, payload_type uint32, payload []byte) (err error) {
	buf := new(bytes.Buffer)
	l := uint32(len(payload))
	buf.WriteString(magic)
	binary.Write(buf, binary.NativeEndian, l)
	binary.Write(buf, binary.NativeEndian, payload_type)
	if l > 0 {
		buf.Write(payload)
	}
	if _, err = conn.Write(buf.Bytes()); err != nil {
		return err
	}
	return nil
}

func read_one_msg(conn *net.UnixConn) (msg_type uint32, payload []byte, err error) {
	header := [14]byte{}
	if _, err = io.ReadFull(conn, header[:]); err != nil {
		return
	}
	h := header[len(magic):]
	var payload_len uint32
	binary.Decode(h, binary.NativeEndian, &payload_len)
	binary.Decode(h[4:], binary.NativeEndian, &msg_type)
	payload = make([]byte, payload_len)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return
	}
	return
}

func get_tree() (ans map[string]any, err error) {
	var conn *net.UnixConn
	if conn, err = connect_to_sway(); err != nil {
		return
	}
	if err = swaymsg(conn, GET_TREE, nil); err != nil {
		return
	}
	var msg_type uint32
	var payload []byte
	if msg_type, payload, err = read_one_msg(conn); err != nil {
		return
	}
	if msg_type != GET_TREE {
		err = fmt.Errorf("Got message of wrong type: %d from sway", msg_type)
		return
	}
	var x map[string]any
	if err = json.Unmarshal(payload, &x); err != nil {
		return
	}
	return x, err
}

func walk_nodes(node map[string]any, callback func(map[string]any)) {
	callback(node)
	for _, collection := range []string{"nodes", "floating_nodes"} {
		if c, ok := node[collection].([]any); ok {
			for _, child := range c {
				if cn, ok := child.(map[string]any); ok {
					walk_nodes(cn, callback)
				}
			}
		}
	}

}

func GetPIDsForGracefulShutdown() []int {
	root, err := get_tree()
	if err != nil {
		return nil
	}
	ans := make([]int, 0, 32)
	walk_nodes(root, func(node map[string]any) {
		if pid, ok := node[`pid`].(float64); ok && pid > 0 {
			if q, ok := node[`type`].(string); ok && q == "con" {
				if app_id, ok := node[`app_id`].(string); ok && app_id != "" {
					ans = append(ans, int(pid))
				}
			}
		}
	})
	return ans
}

func TogglePower(action string, output_name_glob string) (err error) {
	var conn *net.UnixConn
	conn, err = connect_to_sway()
	if err != nil {
		return
	}
	if err = swaymsg(conn, GET_OUTPUTS, []byte("exit")); err != nil {
		return
	}
	msg_type, payload, err := read_one_msg(conn)
	if err != nil {
		return err
	}
	if msg_type != GET_OUTPUTS {
		err = fmt.Errorf("Got unexpected msg_type in response from sway")
		return
	}
	var x []map[string]any
	if err = json.Unmarshal(payload, &x); err != nil {
		return err
	}
	for _, m := range x {
		if name, ok := m[`name`].(string); ok {
			if matched, err := filepath.Match(output_name_glob, name); err != nil {
				return err
			} else if matched {
				if err = swaymsg(conn, RUN_COMMAND, utils.UnsafeStringToBytes(fmt.Sprintf(
					"output %s power %s", name, action))); err != nil {
					return err
				}
			}
		}
	}

	return
}

func ExitSway() (err error) {
	conn, err := connect_to_sway()
	if err != nil {
		return err
	}
	if err = swaymsg(conn, RUN_COMMAND, []byte("exit")); err != nil {
		return
	}
	msg_type, payload, err := read_one_msg(conn)
	if err != nil {
		return err
	}
	if msg_type != RUN_COMMAND {
		err = fmt.Errorf("Got message of wrong type: %d from sway", msg_type)
		return err
	}
	var x []map[string]any
	if err = json.Unmarshal(payload, &x); err != nil {
		return err
	}
	if len(x) != 1 {
		return fmt.Errorf("Got %d responses to exit command", len(x))
	}
	res := x[0]
	if success, ok := res[`success`].(bool); ok {
		if success {
			return nil
		}
		msg := ""
		msg, ok = res[`error`].(string)
		return fmt.Errorf("exit command failed with error: %s", msg)
	} else {
		return fmt.Errorf("Got %#v invalid response to exit command", res)
	}
}

func GetWindowRegions() (regions [][4]int, err error) {
	root, err := get_tree()
	if err != nil {
		return nil, err
	}
	walk_nodes(root, func(node map[string]any) {
		if _, ok := node[`pid`].(float64); ok {
			if visible, ok := node[`visible`].(bool); ok && visible {
				if rect, ok := node[`rect`].(map[string]any); ok {
					regions = append(regions, [4]int{int(rect[`x`].(float64)), int(rect["y"].(float64)), int(rect["width"].(float64)), int(rect["height"].(float64))})
				}
			}
		}

	})
	return
}

func ChangeToWorkspace(name string) (err error) {
	var conn *net.UnixConn
	if conn, err = connect_to_sway(); err != nil {
		return
	}
	err = swaymsg(conn, RUN_COMMAND, []byte("workspace "+name))
	return
}

func SwayBar(set_string func(change, val string)) (err error) {
	var payload []byte
	var msg_type uint32
	var conn *net.UnixConn
	if conn, err = connect_to_sway(); err != nil {
		return
	}
	subscribe_to, _ := json.Marshal([]string{"workspace", "window"})
	if err = swaymsg(conn, SUBSCRIBE, subscribe_to); err != nil {
		return
	}
	var find_focused_window_title func(node map[string]any) string
	find_focused_window_title = func(node map[string]any) string {
		if f, ok := node[`focused`].(bool); ok && f {
			if name, ok := node[`name`].(string); ok && name != "" {
				return name
			}
		}
		for _, key := range []string{"nodes", "floating_nodes"} {
			if fn, ok := node[key].([]any); ok {
				for _, x := range fn {
					if child, ok := x.(map[string]any); ok {
						if name := find_focused_window_title(child); name != "" {
							return name
						}
					}
				}
			}

		}
		return ""
	}

	handle_response := func() {
		var x map[string]any
		var nodes []map[string]any
		switch msg_type {
		case SUBSCRIBE:
			if string(payload) != `{"success": true}` {
				debugprintln(fmt.Errorf("Subscribe to sway events failed with unexpected payload: %#v", string(payload)))
				return
			}
		case GET_WORKSPACES:
			if err = json.Unmarshal(payload, &nodes); err != nil {
				debugprintln(fmt.Errorf("Workspace query failed with unexpected payload: %#v", string(payload)))
				return
			}
			for _, x := range nodes {
				if f, ok := x[`focused`].(bool); ok && f {
					if f, ok := x[`visible`].(bool); ok && f {
						if name, ok := x[`name`].(string); ok {
							_, n, _ := strings.Cut(name, ":")
							set_string("workspace", n)
						}
					}
				}
			}
		case GET_TREE:
			if err = json.Unmarshal(payload, &x); err != nil {
				debugprintln(fmt.Errorf("get_tree query failed with unexpected payload: %#v", string(payload)))
				return
			}
			set_string("title", find_focused_window_title(x))
		case EVENT_WORKSPACE:
			if err = json.Unmarshal(payload, &x); err != nil {
				debugprintln("Failed to parse message of type %x from sway with error: %s", msg_type, err)
				return
			}
			if change, ok := x[`change`].(string); ok && change == `focus` {
				if c, ok := x[`current`].(map[string]any); ok {
					if name, ok := c[`name`].(string); ok {
						_, n, _ := strings.Cut(name, ":")
						set_string("workspace", n)
					}
				}
			}
		case EVENT_WINDOW:
			if err = json.Unmarshal(payload, &x); err != nil {
				debugprintln("Failed to parse message of type %x from sway with error: %s", msg_type, err)
				return
			}
			if change, ok := x[`change`].(string); ok {
				switch change {
				case `focus`:
					if container, ok := x[`container`].(map[string]any); ok {
						if name, ok := container[`name`].(string); ok {
							set_string("title", name)
						}
					}
				case `title`:
					if container, ok := x[`container`].(map[string]any); ok {
						if focused, ok := container[`focused`].(bool); ok && focused {
							if name, ok := container[`name`].(string); ok {
								set_string("title", name)
							}
						}
					}
				}
			}
		default:
			debugprintln("Got unknown message type from sway: %x", msg_type)
		}
	}
	if err = swaymsg(conn, GET_WORKSPACES, nil); err != nil {
		return
	}
	if err = swaymsg(conn, GET_TREE, nil); err != nil {
		return
	}
	go func() {
		var err error
		for {
			if msg_type, payload, err = read_one_msg(conn); err != nil {
				debugprintln("Failed to read message from sway with error: %s", err)
				return
			}
			handle_response()
		}
	}()
	return
} // }}}
