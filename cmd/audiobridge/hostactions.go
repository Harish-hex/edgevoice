package main

import (
	"fmt"
	"os/exec"
	"strings"

	"edgevoice/internal/nlu"
)

// appWhitelist is every APP value in the lexicon: the only apps the assistant may open or close.
var appWhitelist = func() map[string]bool {
	m := map[string]bool{}
	for _, a := range nlu.MustDefault().RoleValues("APP") {
		m[a] = true
	}
	return m
}()

// hostAction performs Mac-side effects requested by the (network-less) container:
// opening/closing whitelisted apps and showing notifications when alarms/timers fire.
func hostAction(ev map[string]any, h *hub) {
	switch ev["kind"] {
	case "app":
		app, _ := ev["app"].(string)
		action, _ := ev["action"].(string)
		if !appWhitelist[app] {
			h.publish(map[string]any{"kind": "app_result", "ok": false, "text": "not allowed: " + app})
			return
		}
		var cmd *exec.Cmd
		if action == "close" {
			cmd = exec.Command("osascript", "-e", fmt.Sprintf("quit app %q", app))
		} else {
			cmd = exec.Command("open", "-a", app)
		}
		out, err := cmd.CombinedOutput()
		res := map[string]any{"kind": "app_result", "ok": err == nil, "text": action + " " + app}
		if err != nil {
			res["text"] = fmt.Sprintf("couldn't %s %s: %s", action, app, strings.TrimSpace(string(out)))
		}
		fmt.Printf("\r%-60s\r[app] %s\n", "", res["text"])
		h.publish(res)
	case "fired":
		text, _ := ev["text"].(string)
		script := fmt.Sprintf("display notification %q with title \"EdgeVoice\" sound name \"Glass\"", text)
		exec.Command("osascript", "-e", script).Start()
	}
}
