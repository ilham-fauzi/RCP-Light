package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// === Data Structures ===

type NetStats struct {
	Interface string
	LastIn    int64
	LastOut   int64
	InSpeed   float64
	OutSpeed  float64
	LastCheck time.Time
}

// === Global Variables ===

// credStore holds credentials for auto-reconnect watchdog
type vpnCred struct {
	user string
	pass string
}

var (
	configDir  string
	openvpnBin string
	profiles   []string
	profileMap map[string]string
	username   string
	miniMode   bool

	globalNetStats = make(map[string]*NetStats)

	// activeConnections tracks profiles that should remain connected (for auto-reconnect)
	activeConnections   = make(map[string]vpnCred)
	activeConnMu        sync.Mutex

	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#5F5CF1")).Padding(0, 1).Bold(true)
	selectedItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF")).Bold(true)
	itemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5CF1")).PaddingLeft(2)
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).PaddingLeft(2)
	keyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF88")).Bold(true)
	descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	footerStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, false, false).BorderForeground(lipgloss.Color("#333333")).MarginTop(1).PaddingTop(1)
	mainBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#5F5CF1")).Padding(1, 2).Width(80)
	miniBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#00FF88")).Padding(0, 1)
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
	midStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#005F87"))
	brightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF")).Bold(true)
	ipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF88")).Italic(true)
	downStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAC1C"))
	upStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#1C93FF"))

	sharedUser string
	sharedPass string
)

// === Logic Functions ===

func initConfig() {
	homeDir, _ := os.UserHomeDir()
	configDir = filepath.Join(homeDir, ".rcp-network")
	os.MkdirAll(configDir, 0755); os.Chmod(configDir, 0755)
	refreshProfiles()
	credPath := filepath.Join(configDir, "username.cred")
	content, err := os.ReadFile(credPath)
	if err == nil { username = strings.TrimSpace(string(content)) } else { username = "vpnuser" }
	loadSharedCredentials()
}

func saveUsername(newUsername string) {
	username = strings.TrimSpace(newUsername)
	credPath := filepath.Join(configDir, "username.cred")
	os.WriteFile(credPath, []byte(username), 0644)
}

func loadSharedCredentials() {
	path := filepath.Join(configDir, "session.cred")
	content, err := os.ReadFile(path)
	if err == nil {
		lines := strings.Split(string(content), "\n")
		if len(lines) >= 2 {
			sharedUser = strings.TrimSpace(lines[0])
			sharedPass = strings.TrimSpace(lines[1])
		}
	}
}

func saveSharedCredentials(u, p string) {
	sharedUser = u
	sharedPass = p
	path := filepath.Join(configDir, "session.cred")
	os.WriteFile(path, []byte(u+"\n"+p), 0600)
}

func refreshProfiles() {
	profileMap = make(map[string]string)
	entries, err := os.ReadDir(configDir)
	if err != nil {
		os.WriteFile("/tmp/rcp-error.log", []byte(fmt.Sprintf("Error reading %s: %v\n", configDir, err)), 0644)
		return
	}
	var newProfiles []string
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if !entry.IsDir() && strings.EqualFold(ext, ".ovpn") {
			name := strings.TrimSuffix(entry.Name(), ext)
			newProfiles = append(newProfiles, name)
			profileMap[name] = entry.Name()
			os.Chmod(filepath.Join(configDir, entry.Name()), 0644)
		}
	}
	profiles = newProfiles
}

func updateAllStats() {
	refreshProfiles()
	for _, p := range profiles {
		if isProfileConnected(p) {
			ip := getVPNIPFromLog(p)
			if _, ok := globalNetStats[p]; !ok { globalNetStats[p] = &NetStats{} }
			globalNetStats[p].Interface = findInterfaceByIP(ip)
			updateNetStats(globalNetStats[p])
		} else { delete(globalNetStats, p) }
	}
}

func startBackgroundSync() {
	go func() {
		for { updateAllStats(); time.Sleep(1 * time.Second) }
	}()
}

func checkEngine() {
	path, err := exec.LookPath("openvpn")
	if err == nil { openvpnBin = path; return }
	possiblePaths := []string{"/opt/homebrew/sbin/openvpn", "/usr/local/sbin/openvpn"}
	for _, p := range possiblePaths {
		if _, err := os.Stat(p); err == nil { openvpnBin = p; return }
	}
	os.Exit(1)
}

func sanitizeOvpn(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil { return err }
	lines := strings.Split(string(content), "\n")
	var newLines []string
	// NOTE: persist-key and persist-tun are intentionally NOT removed — they are essential
	// for connection resilience when network is interrupted (laptop sleep, brief disconnects).
	// persist-key  → keeps TLS keys in memory so re-auth is not needed after network recovery
	// persist-tun  → keeps TUN interface open so the tunnel can resume without full restart
	reRemove := regexp.MustCompile(`(?i)^(--)?(client-cert-not-required|verify-client-cert|route-method|route-delay)`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") { newLines = append(newLines, line); continue }
		if strings.HasPrefix(strings.ToLower(trimmed), "auth-user-pass") { continue }
		if reRemove.MatchString(trimmed) { continue }
		newLines = append(newLines, line)
	}
	newLines = append(newLines, "auth-user-pass")
	return os.WriteFile(filePath, []byte(strings.Join(newLines, "\n")), 0644)
}

func setupTouchID() {
	content, _ := os.ReadFile("/etc/pam.d/sudo")
	if strings.Contains(string(content), "pam_tid.so") { return }
	cmd := "sed -i '' '1i\\\nauth       sufficient     pam_tid.so\\n' /etc/pam.d/sudo"
	exec.Command("osascript", "-e", fmt.Sprintf("do shell script \"%s\" with administrator privileges", cmd)).Run()
}

func setupSudoers() {
	ruleFile := "/etc/sudoers.d/rcp-light"
	if _, err := os.Stat(ruleFile); err == nil { return }
	
	paths := map[string]bool{
		"/opt/homebrew/sbin/openvpn": true,
		"/usr/local/sbin/openvpn":    true,
		"/usr/bin/kill":              true,
		"/bin/kill":                  true,
	}
	if openvpnBin != "" { paths[openvpnBin] = true }
	
	var pathList []string
	for p := range paths { pathList = append(pathList, p) }
	
	rule := fmt.Sprintf("%%admin ALL=(ALL) NOPASSWD: %s", strings.Join(pathList, ", "))
	cmd := fmt.Sprintf("echo '%s' > /tmp/rcp-sudoers && chmod 440 /tmp/rcp-sudoers && chown root:wheel /tmp/rcp-sudoers && mv /tmp/rcp-sudoers %s", rule, ruleFile)
	
	exec.Command("osascript", "-e", fmt.Sprintf("do shell script \"%s\" with administrator privileges", cmd)).Run()
}

func closeTerminal() {
	// Use a background script with a small delay to ensure the process exits 
	// before the window is told to close. This is the most reliable way on macOS.
	script := `
	delay 0.1
	tell application "Terminal"
		repeat with w in windows
			try
				if custom title of w is "RCP NETWORK" then
					close w saving no
					return
				end if
			end try
		end repeat
	end tell
	try
		tell application "iTerm"
			repeat with w in windows
				repeat with t in tabs of w
					repeat with s in sessions of t
						if name of s contains "RCP NETWORK" then
							close s
							return
						end if
					end repeat
				end repeat
			end repeat
		end tell
	end try`
	exec.Command("osascript", "-e", script).Start()
}

func main() {
	initConfig()
	checkEngine()
	setupSudoers()

	// Subprocess mode: show login window for a specific profile (called from tray click)
	if len(os.Args) > 2 && os.Args[1] == "connect-ui" {
		profile := os.Args[2]
		refreshProfiles()
		defU := sharedUser; if defU == "" { defU = username }
		res := showLoginWindow(profile, defU, sharedPass)
		if !res.canceled {
			if res.applyAll {
				saveSharedCredentials(res.user, res.password)
			}
			connectVPN(profile, res.user, res.password)
		}
		return
	}

	if len(os.Args) > 1 && (os.Args[1] == "ui" || os.Args[1] == "mini" || os.Args[1] == "dashboard") {
		if os.Args[1] == "mini" { miniMode = true }
		startBackgroundSync()
		if os.Args[1] == "dashboard" {
			d := &Dashboard{}
			d.Run()
			return
		}

		if os.Args[1] == "ui" {
			rows := 15 + len(profiles)
			if rows < 16 { rows = 16 }
			if rows > 35 { rows = 35 }
			fmt.Printf("\033[8;%d;85t", rows)
			fmt.Printf("\033]0;RCP Light\007")
		}

		tea.NewProgram(initialModel()).Run()
	} else {
		startBackgroundSync()
		setupTouchID()
		startNativeTray()
	}
}

func openDashboard() {
	exe, _ := os.Executable()
	exec.Command(exe, "dashboard").Start()
}

var (
	activeLoginCmd *exec.Cmd
	activeLoginMu  sync.Mutex
)

// openLoginWindowSubprocess launches the login window in a fresh subprocess.
// This is required because NSWindow must be created on the main thread,
// which is already owned by systray in the tray process.
func openLoginWindowSubprocess(profile string) {
	activeLoginMu.Lock()
	defer activeLoginMu.Unlock()
	if activeLoginCmd != nil {
		if activeLoginCmd.Process != nil {
			activeLoginCmd.Process.Kill()
		}
		activeLoginCmd = nil
	}

	exe, _ := os.Executable()
	cmd := exec.Command(exe, "connect-ui", profile)
	cmd.Start()
	activeLoginCmd = cmd

	go func(c *exec.Cmd) {
		c.Wait()
		activeLoginMu.Lock()
		if activeLoginCmd == c {
			activeLoginCmd = nil
		}
		activeLoginMu.Unlock()
	}(cmd)
}

func openTUI() {
	exe, _ := os.Executable()
	rows := 15 + len(profiles)
	if rows < 16 { rows = 16 }
	if rows > 35 { rows = 35 }

	checkScript := `
		tell application "System Events"
			set isRunning to (count of (every process whose name is "Terminal")) > 0
		end tell
		if isRunning then
			tell application "Terminal"
				repeat with w in windows
					try
						if custom title of w is "RCP NETWORK" then
							set miniaturized of w to false
							set index of w to 1
							tell application "System Events" to set frontmost of process "Terminal" to true
							return "FOUND"
						end if
					end try
				end repeat
			end tell
		end if
		return "NOT_FOUND"
	`
	out, _ := exec.Command("osascript", "-e", checkScript).Output()
	if strings.TrimSpace(string(out)) == "FOUND" {
		return
	}

	scriptPath := filepath.Join(os.TempDir(), "rcp_tui.command")
	scriptContent := fmt.Sprintf(`#!/bin/bash
printf '\033c'
echo -ne "\033]0;RCP NETWORK\007"
echo -ne "\033[8;%d;85t"
"%s" ui
osascript -e 'tell application "Terminal" to close (every window whose custom title is "RCP NETWORK") saving no' &
`, rows, exe)
	os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	exec.Command("open", scriptPath).Run()
}

func isProfileConnected(profile string) bool {
	pidPath := filepath.Join(configDir, profile+".pid")
	content, err := os.ReadFile(pidPath)
	if err != nil { return false }
	pid := strings.TrimSpace(string(content))
	if pid == "" { return false }
	cmd := exec.Command("ps", "-p", pid, "-o", "comm=")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, _ := cmd.Output()
	return strings.Contains(string(out), "openvpn")
}

func getVPNIPFromLog(profile string) string {
	logPath := filepath.Join(configDir, profile+".log")
	content, err := os.ReadFile(logPath)
	if err != nil { return "" }
	re := regexp.MustCompile(`ifconfig\s+\w+\s+(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindAllStringSubmatch(string(content), -1)
	if len(matches) > 0 { return matches[len(matches)-1][1] }
	return ""
}

func findInterfaceByIP(ip string) string {
	if ip == "" { return "" }
	cmd := exec.Command("ifconfig")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	if err != nil { return "" }
	lines := strings.Split(string(out), "\n")
	var currentIface string
	for _, line := range lines {
		if !strings.HasPrefix(line, "\t") {
			parts := strings.Split(line, ":")
			if len(parts) > 0 { currentIface = parts[0] }
		}
		if strings.Contains(line, ip) { return currentIface }
	}
	return ""
}

func updateNetStats(s *NetStats) {
	if s.Interface == "" { return }
	cmd := exec.Command("netstat", "-I", s.Interface, "-b", "-n", "-f", "link")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	if err != nil { return }
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 { return }
	fields := strings.Fields(lines[1])
	if len(fields) < 9 { return }
	ibytes, _ := strconv.ParseInt(fields[5], 10, 64)
	obytes, _ := strconv.ParseInt(fields[8], 10, 64)
	now := time.Now()
	if !s.LastCheck.IsZero() {
		duration := now.Sub(s.LastCheck).Seconds()
		if duration > 0 {
			rawIn := float64(ibytes-s.LastIn) / duration
			rawOut := float64(obytes-s.LastOut) / duration
			alpha := 0.35
			s.InSpeed = (rawIn * alpha) + (s.InSpeed * (1 - alpha))
			s.OutSpeed = (rawOut * alpha) + (s.OutSpeed * (1 - alpha))
			if s.InSpeed < 50 { s.InSpeed = 0 }; if s.OutSpeed < 50 { s.OutSpeed = 0 }
		}
	}
	s.LastIn, s.LastOut, s.LastCheck = ibytes, obytes, now
}

func connectVPN(profile string, user string, password string) error {
	ovpnFile := profileMap[profile]
	ovpnPath := filepath.Join(configDir, ovpnFile)
	sanitizeOvpn(ovpnPath)
	logPath := filepath.Join(configDir, profile+".log")
	pidPath := filepath.Join(configDir, profile+".pid")
	os.Remove(logPath); os.Remove(pidPath)
	os.WriteFile(logPath, []byte(""), 0666)
	tmpAuthPath := filepath.Join(configDir, ".tmp_auth_"+profile)
	os.WriteFile(tmpAuthPath, []byte(fmt.Sprintf("%s\n%s", user, password)), 0600)

	cmd := exec.Command("sudo", openvpnBin,
		"--config", ovpnPath,
		"--auth-user-pass", tmpAuthPath,
		"--daemon",
		"--log", logPath,
		"--writepid", pidPath,
		// Persistence across network interruptions (laptop sleep, brief disconnects)
		"--persist-key",         // keep TLS keys in memory; no re-auth needed after recovery
		"--persist-tun",         // keep TUN device open across reconnects
		// Keepalive: ping every 10s, restart tunnel if no response for 120s
		"--keepalive", "10", "120",
		// Retry DNS resolution indefinitely — critical when internet returns after a drop
		"--resolv-retry", "infinite",
		// Retry connection every 5 seconds on failure
		"--connect-retry", "5",
		"--connect-retry-max", "0", // 0 = unlimited retries
		// Disable inactivity timeout
		"--inactive", "0",
	)
	_, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpAuthPath)
		return fmt.Errorf("OpenVPN Start Error: %v", err)
	}

	// Wait and check if it survives or fails auth
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if !isProfileConnected(profile) {
			logContent, _ := os.ReadFile(logPath)
			if strings.Contains(string(logContent), "AUTH_FAILED") {
				os.Remove(tmpAuthPath)
				return fmt.Errorf("AUTHENTICATION FAILED")
			}
			os.Remove(tmpAuthPath)
			return fmt.Errorf("Connection terminated unexpectedly")
		}
		logContent, _ := os.ReadFile(logPath)
		if strings.Contains(string(logContent), "Initialization Sequence Completed") {
			break
		}
	}

	// Register credentials for watchdog auto-reconnect
	activeConnMu.Lock()
	activeConnections[profile] = vpnCred{user: user, pass: password}
	activeConnMu.Unlock()

	// Start per-profile watchdog goroutine
	go vpnWatchdog(profile)

	// Clean up auth file after a short delay
	go func() { time.Sleep(15 * time.Second); os.Remove(tmpAuthPath) }()
	return nil
}

// vpnWatchdog monitors a connected profile and auto-reconnects if it drops unexpectedly.
// It stops when the profile is no longer in activeConnections (i.e., user disconnected).
func vpnWatchdog(profile string) {
	// Grace period before first check
	time.Sleep(10 * time.Second)
	for {
		activeConnMu.Lock()
		cred, shouldWatch := activeConnections[profile]
		activeConnMu.Unlock()

		if !shouldWatch {
			// User explicitly disconnected — stop watchdog
			return
		}

		if !isProfileConnected(profile) {
			// Connection dropped unexpectedly — attempt reconnect
			// Brief wait to let network settle (e.g. after wake from sleep)
			time.Sleep(5 * time.Second)

			activeConnMu.Lock()
			_, stillActive := activeConnections[profile]
			activeConnMu.Unlock()
			if !stillActive { return }

			// Try to reconnect (ignore errors — watchdog will retry next cycle)
			_ = connectVPNDirect(profile, cred.user, cred.pass)
		}

		time.Sleep(15 * time.Second)
	}
}

// connectVPNDirect reconnects without re-registering the watchdog (called from watchdog itself).
func connectVPNDirect(profile string, user string, password string) error {
	ovpnFile := profileMap[profile]
	if ovpnFile == "" { return fmt.Errorf("profile not found") }
	ovpnPath := filepath.Join(configDir, ovpnFile)
	logPath := filepath.Join(configDir, profile+".log")
	pidPath := filepath.Join(configDir, profile+".pid")
	os.Remove(logPath); os.Remove(pidPath)
	os.WriteFile(logPath, []byte(""), 0666)
	tmpAuthPath := filepath.Join(configDir, ".tmp_auth_"+profile)
	os.WriteFile(tmpAuthPath, []byte(fmt.Sprintf("%s\n%s", user, password)), 0600)

	cmd := exec.Command("sudo", openvpnBin,
		"--config", ovpnPath,
		"--auth-user-pass", tmpAuthPath,
		"--daemon",
		"--log", logPath,
		"--writepid", pidPath,
		"--persist-key",
		"--persist-tun",
		"--keepalive", "10", "120",
		"--resolv-retry", "infinite",
		"--connect-retry", "5",
		"--connect-retry-max", "0",
		"--inactive", "0",
	)
	_, err := cmd.CombinedOutput()
	go func() { time.Sleep(15 * time.Second); os.Remove(tmpAuthPath) }()
	return err
}

// disconnectVPN explicitly stops a VPN connection and deregisters it from the watchdog,
// so the auto-reconnect goroutine will exit cleanly.
func disconnectVPN(profile string) {
	// Deregister FIRST so watchdog knows this is intentional and stops
	activeConnMu.Lock()
	delete(activeConnections, profile)
	activeConnMu.Unlock()

	pidPath := filepath.Join(configDir, profile+".pid")
	if content, err := os.ReadFile(pidPath); err == nil {
		pid := strings.TrimSpace(string(content))
		if pid != "" { exec.Command("sudo", "kill", pid).Run() }
		os.Remove(pidPath)
	}
}

// trayIconData is moved to icon.go usually, let's make sure it's available.
// If it's missing, I'll need to find where it was defined.

const (
	stateSelect = iota
	stateConnUser
	stateConnPass
	stateConnecting
	stateError
	stateConfirmDelete
	stateRenameImport
	stateUsername
)

type model struct {
	cursor     int
	choices    []string
	connected  map[string]bool
	ips        map[string]string
	offsets    map[string]int
	state      int
	input      textinput.Model
	statusMsg  string
	pulseState bool
	frame      int
	importPath string
	
	tempUser   string
	tempPass   string
	applyAll   bool
}

func initialModel() model {
	ti := textinput.New(); ti.Placeholder = "Enter..."; ti.EchoMode = textinput.EchoNormal; ti.Focus()
	return model{choices: profiles, input: ti, connected: make(map[string]bool), ips: make(map[string]string), offsets: make(map[string]int)}
}

func (m model) Init() tea.Cmd { return tea.Batch(textinput.Blink, tea.Tick(time.Millisecond*80, func(t time.Time) tea.Msg { return t })) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case time.Time:
		m.frame++
		if m.frame%10 == 0 { m.pulseState = !m.pulseState }
		m.connected = make(map[string]bool)
		for _, p := range profiles {
			m.connected[p] = isProfileConnected(p)
			if m.connected[p] { m.ips[p] = getVPNIPFromLog(p) }
			if _, ok := m.offsets[p]; !ok { m.offsets[p] = rand.Intn(100) }
		}
		return m, tea.Tick(time.Millisecond*80, func(t time.Time) tea.Msg { return t })
	case tea.KeyMsg:
		if m.state == stateConnecting { return m, nil }
		switch msg.String() {
		case "ctrl+c", "q": 
			closeTerminal()
			return m, tea.Quit
		case "up", "k":
			if m.state == stateSelect || m.state == stateError {
				if m.cursor > 0 { m.cursor-- }
			}
		case "down", "j":
			if m.state == stateSelect || m.state == stateError {
				if m.cursor < len(m.choices)-1 { m.cursor++ }
			}
		case "i":
			if m.state == stateSelect || m.state == stateError {
				script := `POSIX path of (choose file with prompt "Select an OVPN profile" of type {"ovpn", "public.data"})`
				out, err := exec.Command("osascript", "-e", script).Output()
				if err == nil {
					src := strings.TrimSpace(string(out))
					if src != "" {
						base := strings.TrimSuffix(filepath.Base(src), ".ovpn")
						m.importPath = src; m.input.EchoMode = textinput.EchoNormal
						suggest := base; if len(suggest) > 10 { suggest = suggest[:10] }
						m.input.SetValue(suggest); m.state = stateRenameImport; m.input.Focus()
						return m, nil
					}
				}
			}
		case "u":
			if m.state == stateSelect || m.state == stateError {
				m.state = stateUsername; m.input.EchoMode = textinput.EchoNormal; m.input.SetValue(username); m.input.Focus()
				return m, nil
			}
		case "x": if len(m.choices) > 0 && (m.state == stateSelect || m.state == stateError) { m.state = stateConfirmDelete }
		case "y":
			if m.state == stateConfirmDelete {
				p := m.choices[m.cursor]
				disconnectVPN(p) // also stops watchdog
				os.Remove(filepath.Join(configDir, profileMap[p]))
				m.state = stateSelect; if m.cursor >= len(m.choices) && m.cursor > 0 { m.cursor-- }
			}
		case "n", "esc": m.state = stateSelect; m.statusMsg = ""
		case "tab":
			if m.state == stateConnUser || m.state == stateConnPass {
				m.applyAll = !m.applyAll
			}
		case "enter":
			if m.state == stateSelect || m.state == stateError {
				if len(m.choices) == 0 { return m, nil }
				p := m.choices[m.cursor]
				if isProfileConnected(p) {
					disconnectVPN(p) // stops watchdog + kills process
				} else { 
					m.state = stateConnUser; m.input.EchoMode = textinput.EchoNormal
					m.input.Prompt = ""; val := sharedUser; if val == "" { val = username }
					m.input.SetValue(val); m.input.Focus()
					m.input.SetCursor(len(val))
					m.applyAll = (sharedPass != "")
				}
			} else if m.state == stateConnUser {
				m.tempUser = m.input.Value()
				m.state = stateConnPass; m.input.EchoMode = textinput.EchoPassword
				m.input.Prompt = ""; m.input.SetValue(""); m.input.Focus()
			} else if m.state == stateConnPass {
				m.tempPass = m.input.Value()
				// If input is empty but we have a shared password, use the shared one
				if m.tempPass == "" && sharedPass != "" {
					m.tempPass = sharedPass
				}
				m.state = stateConnecting; p := m.choices[m.cursor]
				if m.applyAll { saveSharedCredentials(m.tempUser, m.tempPass) }
				return m, func() tea.Msg { if err := connectVPN(p, m.tempUser, m.tempPass); err != nil { return err }; return p }
			} else if m.state == stateRenameImport {
				newName := strings.TrimSpace(m.input.Value())
				if newName != "" {
					newName = strings.TrimSuffix(newName, ".ovpn"); dst := filepath.Join(configDir, newName+".ovpn")
					exec.Command("cp", m.importPath, dst).Run(); m.state = stateSelect
				}
			} else if m.state == stateUsername {
				saveUsername(m.input.Value()); m.state = stateSelect
			}
		}
	case error: m.state = stateError; m.statusMsg = msg.Error()
	case string: m.state = stateSelect; m.statusMsg = ""
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func getMiniScanner(frame int, width int) string {
	var res strings.Builder
	pos := frame % (width * 2)
	for i := 0; i < width; i++ {
		dist := math.Abs(float64(i) - float64(pos))
		if pos > width { dist = math.Abs(float64(i) - float64(width*2-pos)) }
		if dist < 1.5 { res.WriteString(brightStyle.Render("━"))
		} else if dist < 3 { res.WriteString(midStyle.Render("━"))
		} else { res.WriteString(dimStyle.Render("─")) }
	}
	return res.String()
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec < 50 { return "0 B  " }
	if bytesPerSec < 1024 { return fmt.Sprintf("%0.0f B ", bytesPerSec) }
	if bytesPerSec < 1024*1024 { return fmt.Sprintf("%0.0f KB", bytesPerSec/1024) }
	return fmt.Sprintf("%0.1f MB", bytesPerSec/1024/1024)
}

func truncate(s string, max int) string {
	if len(s) <= max { return s }
	return s[:max] + "..."
}

func (m model) View() string {
	if miniMode {
		var activeLines []string
		for _, p := range profiles {
			if isProfileConnected(p) {
				name := truncate(p, 10); ip := getVPNIPFromLog(p)
				scanner := getMiniScanner(m.frame + m.offsets[p], 12)
				activeLines = append(activeLines, fmt.Sprintf("🟢 %-10s (%-15s) %s", name, ip, scanner))
			}
		}
		if len(activeLines) == 0 { return miniBoxStyle.Render("RCP: No Active VPN") + "\n" }
		return miniBoxStyle.Render(strings.Join(activeLines, "\n")) + "\n"
	}
	var body string
	
	header := lipgloss.JoinHorizontal(lipgloss.Center,
		titleStyle.Render(" RCP "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5CF1")).Padding(0, 1).Render("Light V1.2.0"),
	) + "\n\n"

	if m.state == stateRenameImport {
		body = fmt.Sprintf("Import Profile:\n\nName (Max 10 chars):\n%s\n\n[Enter] Save   [Esc] Cancel", m.input.View())
	} else if m.state == stateConfirmDelete {
		body = fmt.Sprintf("Delete profile: %s?\n\n[y] Yes  [n] No", m.choices[m.cursor])
	} else if m.state == stateSelect || m.state == stateError {
		var listLines []string
		const innerWidth = 71
		if len(m.choices) == 0 {
			listLines = append(listLines, " (No profiles found. Press 'i' to import)")
		} else {
			for i, p := range m.choices {
				statusIcon := "  "; ipInfo := ""; scanner := ""; speedInfo := ""; name := truncate(p, 10)
				connected := isProfileConnected(p)
				if connected {
					if m.pulseState { statusIcon = "🟢" } else { statusIcon = "🔵" }
					ip := getVPNIPFromLog(p); if ip != "" { ipInfo = " (" + ip + ")" }
					scanner = getMiniScanner(m.frame + m.offsets[p], 12)
					if s, ok := globalNetStats[p]; ok {
						speedInfo = fmt.Sprintf(" %s %s %s %s", downStyle.Render("↓"), formatSpeed(s.InSpeed), upStyle.Render("↑"), formatSpeed(s.OutSpeed))
					}
				}
				prefix := "  "; if m.cursor == i { prefix = "> " }
				leftPartText := fmt.Sprintf("%s%s %s%s", prefix, statusIcon, name, ipInfo)
				var leftPartRendered string
				if m.cursor == i { leftPartRendered = selectedItemStyle.Render(fmt.Sprintf("> %s %s", statusIcon, name)) + ipStyle.Render(ipInfo)
				} else { leftPartRendered = itemStyle.Render(fmt.Sprintf("  %s %s", statusIcon, name)) + ipStyle.Render(ipInfo) }
				rawLeftLen := len(leftPartText); rawRightLen := 12 + 22
				if !connected { rawRightLen = 0 }
				paddingCount := innerWidth - rawLeftLen - rawRightLen
				if paddingCount < 1 { paddingCount = 1 }
				padding := strings.Repeat(" ", paddingCount)
				listLines = append(listLines, leftPartRendered+padding+scanner+speedInfo)
			}
		}
		body = strings.Join(listLines, "\n")
		var statusText string
		activeCount := 0
		for _, p := range profiles { if isProfileConnected(p) { activeCount++ } }
		if len(m.choices) == 0 { statusText = "Status: No Profiles"
		} else if m.state == stateError { statusText = "Error: " + m.statusMsg
		} else { statusText = fmt.Sprintf("Active: %d profiles", activeCount) }
		if m.state == stateError { body += "\n\n" + errorStyle.Render(statusText)
		} else { body += "\n\n" + statusStyle.Render(statusText) }
	} else if m.state == stateConnecting {
		body = fmt.Sprintf("Connecting to %s...\n\nVerifying connection...", m.choices[m.cursor])
	} else if m.state == stateUsername {
		body = fmt.Sprintf("Edit Username:\n\n%s\n\n[Enter] Save   [Esc] Cancel", m.input.View())
	} else if m.state == stateConnUser || m.state == stateConnPass {
		userDisplay := m.tempUser; if m.state == stateConnUser { userDisplay = m.input.View() }
		passDisplay := strings.Repeat("*", len(m.tempPass)); if m.state == stateConnPass { passDisplay = m.input.View() }
		cb := "[ ]"; if m.applyAll { cb = "[" + brightStyle.Render("x") + "]" }
		body = fmt.Sprintf("Profile: %s\n\nUsername: %s\nPassword: %s\n\n%s Apply for this session [Tab]\n\n[Enter] Continue", 
			brightStyle.Render(m.choices[m.cursor]), userDisplay, passDisplay, cb)
	} else {
		body = fmt.Sprintf("Profile: %s\n\nPassword Required:\n%s", m.choices[m.cursor], m.input.View())
	}
	fNav := keyStyle.Render("[↑↓]") + descStyle.Render(" Nav")
	fTgl := keyStyle.Render("[Enter]") + descStyle.Render(" Tgl")
	fUsr := keyStyle.Render("[u]") + descStyle.Render(" Usr")
	fImp := keyStyle.Render("[i]") + descStyle.Render(" Imp")
	fDel := keyStyle.Render("[x]") + descStyle.Render(" Del")
	fQuit := keyStyle.Render("[q]") + descStyle.Render(" Quit")
	footer := footerStyle.Render(fmt.Sprintf(" %s   %s   %s   %s   %s   %s ", fNav, fTgl, fUsr, fImp, fDel, fQuit))
	return mainBoxStyle.Render(header + body + footer) + "\n"
}

func runTUI() { tea.NewProgram(initialModel()).Run() }
