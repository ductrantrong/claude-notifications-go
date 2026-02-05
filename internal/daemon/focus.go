//go:build linux

// ABOUTME: Window focus methods for Linux desktop environments.
// ABOUTME: Implements a fallback chain to focus windows on GNOME, KDE, Sway, and other compositors.
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FocusMethod represents a method for focusing a window
type FocusMethod struct {
	Name string
	Fn   func(terminalName string) error
}

// GetFocusMethods returns the ordered list of focus methods to try
func GetFocusMethods() []FocusMethod {
	return []FocusMethod{
		{"activate-window-by-title extension", TryActivateWindowByTitle},
		{"GNOME Shell Eval (by window title)", TryGnomeShellEvalByTitle},
		{"GNOME Shell Eval (by app)", TryGnomeShellEval},
		{"GNOME Shell FocusApp", TryGnomeFocusApp},
		{"wlrctl", TryWlrctl},
		{"kdotool", TryKdotool},
	}
}

// TryFocus attempts to focus a window using available tools.
// It tries each method in order until one succeeds.
func TryFocus(terminalName string) error {
	methods := GetFocusMethods()

	var lastErr error
	for _, method := range methods {
		if err := method.Fn(terminalName); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	return fmt.Errorf("all focus methods failed, last error: %v", lastErr)
}

// TryActivateWindowByTitle uses the activate-window-by-title GNOME extension.
// https://extensions.gnome.org/extension/5021/activate-window-by-title/
// This method does NOT require unsafe_mode and works on GNOME 42+.
func TryActivateWindowByTitle(terminalName string) error {
	searchTerm := GetSearchTerm(terminalName)

	cmd := exec.Command("busctl", "--user", "call",
		"org.gnome.Shell",
		"/de/lucaswerkmeister/ActivateWindowByTitle",
		"de.lucaswerkmeister.ActivateWindowByTitle",
		"activateBySubstring", "s", searchTerm,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("activate-window-by-title extension not available: %w, output: %s", err, string(output))
	}
	return nil
}

// TryGnomeShellEvalByTitle uses GNOME Shell's Eval to find and focus window by title.
// Requires unsafe_mode or development-tools enabled.
func TryGnomeShellEvalByTitle(terminalName string) error {
	searchTerm := GetSearchTerm(terminalName)

	// JavaScript to find window by title and activate it
	js := fmt.Sprintf(`
		(function() {
			let start = Date.now();
			let found = false;
			global.get_window_actors().forEach(function(actor) {
				let win = actor.get_meta_window();
				let title = win.get_title() || '';
				if (title.indexOf('%s') !== -1) {
					win.activate(start);
					found = true;
				}
			});
			return found ? 'activated' : 'no matching window';
		})()
	`, searchTerm)

	cmd := exec.Command("gdbus", "call",
		"--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		js,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gdbus Eval failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "no matching window") {
		return fmt.Errorf("no window with title containing %q", searchTerm)
	}
	if strings.Contains(outputStr, "false") && !strings.Contains(outputStr, "activated") {
		return fmt.Errorf("Shell.Eval blocked (GNOME 41+ security) - install unsafe-mode-menu extension or activate-window-by-title extension")
	}

	return nil
}

// TryGnomeShellEval uses GNOME Shell's Eval method to activate an app.
// Requires unsafe_mode or development-tools enabled.
func TryGnomeShellEval(terminalName string) error {
	appID := GetAppID(terminalName)

	// JavaScript to find and activate the app's windows
	js := fmt.Sprintf(`
		(function() {
			let app = Shell.AppSystem.get_default().lookup_app('%s');
			if (app) {
				app.activate();
				return 'activated';
			}
			return 'app not found';
		})()
	`, appID)

	cmd := exec.Command("gdbus", "call",
		"--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		js,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gdbus Eval failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "app not found") {
		return fmt.Errorf("app not found via Shell.Eval")
	}
	if strings.Contains(outputStr, "false") && !strings.Contains(outputStr, "activated") {
		return fmt.Errorf("Shell.Eval blocked (GNOME 41+ security) - install unsafe-mode-menu extension or activate-window-by-title extension")
	}

	return nil
}

// TryGnomeFocusApp uses GNOME Shell's FocusApp method (available since GNOME 45).
func TryGnomeFocusApp(terminalName string) error {
	appID := GetAppID(terminalName)

	cmd := exec.Command("gdbus", "call",
		"--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.FocusApp",
		appID,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gdbus FocusApp failed: %w, output: %s", err, string(output))
	}
	return nil
}

// TryWlrctl uses wlrctl for wlroots-based compositors (Sway, etc.).
func TryWlrctl(terminalName string) error {
	if _, err := exec.LookPath("wlrctl"); err != nil {
		return fmt.Errorf("wlrctl not installed")
	}

	// Try app_id first (more reliable)
	cmd := exec.Command("wlrctl", "toplevel", "focus", "app_id:code")
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	// Fallback to title
	searchTerm := GetSearchTerm(terminalName)
	cmd = exec.Command("wlrctl", "toplevel", "focus", "title:"+searchTerm)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wlrctl failed: %w, output: %s", err, string(output))
	}
	return nil
}

// TryKdotool uses kdotool for KDE Plasma.
func TryKdotool(terminalName string) error {
	if _, err := exec.LookPath("kdotool"); err != nil {
		return fmt.Errorf("kdotool not installed")
	}

	// Search by class
	searchCmd := exec.Command("kdotool", "search", "--class", "code")
	output, err := searchCmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil || outputStr == "" {
		return fmt.Errorf("no windows found via kdotool")
	}

	windowIDs := strings.Split(outputStr, "\n")

	cmd := exec.Command("kdotool", "windowactivate", windowIDs[0])
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kdotool windowactivate failed: %w", err)
	}
	return nil
}

// GetAppID returns the .desktop app ID for a terminal name.
func GetAppID(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		return "code.desktop"
	case "gnome-terminal":
		return "org.gnome.Terminal.desktop"
	case "konsole":
		return "org.kde.konsole.desktop"
	case "alacritty":
		return "Alacritty.desktop"
	case "kitty":
		return "kitty.desktop"
	case "wezterm":
		return "org.wezfurlong.wezterm.desktop"
	case "tilix":
		return "com.gexperts.Tilix.desktop"
	case "terminator":
		return "terminator.desktop"
	default:
		return strings.ToLower(terminalName) + ".desktop"
	}
}

// GetSearchTerm returns a window title search term for a terminal name.
func GetSearchTerm(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode":
		return "Visual Studio Code"
	case "gnome-terminal":
		return "Terminal"
	default:
		return terminalName
	}
}

// GetTerminalName detects the current terminal from environment variables.
func GetTerminalName() string {
	// Try TERM_PROGRAM first (set by many terminals)
	if termProg := os.Getenv("TERM_PROGRAM"); termProg != "" {
		return termProg
	}

	// Check VS Code indicators
	if os.Getenv("VSCODE_INJECTION") != "" || os.Getenv("TERM_PROGRAM_VERSION") != "" {
		return "Code"
	}

	// Fallback to generic terminal
	return "Terminal"
}

// DetectFocusTools returns a map of available focus tools.
func DetectFocusTools() map[string]bool {
	tools := map[string]bool{}

	// Check command-line tools
	for _, tool := range []string{"wlrctl", "kdotool", "gdbus", "busctl"} {
		_, err := exec.LookPath(tool)
		tools[tool] = err == nil
	}

	// Check GNOME activate-window-by-title extension
	cmd := exec.Command("busctl", "--user", "introspect",
		"org.gnome.Shell",
		"/de/lucaswerkmeister/ActivateWindowByTitle",
	)
	output, err := cmd.CombinedOutput()
	tools["activate-window-by-title"] = err == nil && strings.Contains(string(output), "activateBySubstring")

	return tools
}
