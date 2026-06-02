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
	"net"
	"net/http"
	"crypto/tls"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/getlantern/systray"
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

type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Type      string    `json:"type"` // info, success, warning
}

type HostRouteEntry struct {
	IP        string    `json:"ip"`
	Profile   string    `json:"profile"`
	Timestamp time.Time `json:"timestamp"`
}

// === Global Variables ===

// credStore holds credentials for auto-reconnect watchdog
type vpnCred struct {
	user string
	pass string
}

type trayProfileItem struct {
	parent  *systray.MenuItem
	connect *systray.MenuItem
	delete  *systray.MenuItem
}

type TrayProfileItem struct {
	item    *systray.MenuItem
	profile string
}

var (
	configDir  string
	openvpnBin string
	profiles   []string
	profileMap map[string]string
	username   string
	miniMode   bool

	globalNetStats = make(map[string]*NetStats)
	vpnStateMu     sync.Mutex

	// activeConnections tracks profiles that should remain connected (for auto-reconnect)
	activeConnections   = make(map[string]vpnCred)
	activeConnMu        sync.Mutex

	// globalLatencies stores current latency in ms for each active profile
	globalLatencies     = make(map[string]float64)
	globalLatenciesMu   sync.Mutex

	// currentActiveInterface is the interface currently handling global traffic
	currentActiveInterface string
	currentActiveIfaceMu   sync.Mutex

	// Smart Orchestrator state for anti-flapping
	lastWinner         string
	lastWinnerLatency  float64
	orchestratorMu     sync.Mutex

	trayItems   [50]*TrayProfileItem
	trayItemsMu sync.Mutex

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

	auditLogs []AuditEntry
	auditMu   sync.Mutex

	hostRouteCache   = make(map[string]HostRouteEntry) // Domain/IP -> Entry
	hostRouteCacheMu sync.Mutex
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
	os.WriteFile(filepath.Join(configDir, "username.cred"), []byte(username), 0644)
}

func addAuditLog(msg string, entryType string) {
	auditMu.Lock()
	defer auditMu.Unlock()
	auditLogs = append(auditLogs, AuditEntry{
		Timestamp: time.Now(),
		Message:   msg,
		Type:      entryType,
	})
	if len(auditLogs) > 50 {
		auditLogs = auditLogs[len(auditLogs)-50:]
	}
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

type profileCredState struct {
	user, pass                  string
	su, sp, applyAll, saveProf bool
}

func getProfileCredState(profile string) profileCredState {
	s := profileCredState{}
	path := filepath.Join(configDir, profile+".cred")
	content, err := os.ReadFile(path)
	if err != nil { return s }
	lines := strings.Split(string(content), "\n")
	if len(lines) >= 2 { s.user = strings.TrimSpace(lines[0]); s.pass = strings.TrimSpace(lines[1]) }
	hasCb := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch line {
		case "su=1": s.su = true; hasCb = true
		case "su=0": hasCb = true
		case "sp=1": s.sp = true; hasCb = true
		case "sp=0": hasCb = true
		case "a=1": s.applyAll = true; hasCb = true
		case "a=0": hasCb = true
		case "f=1": s.saveProf = true; hasCb = true
		case "f=0": hasCb = true
		}
	}
	if !hasCb { if s.user != "" { s.su = true }; if s.pass != "" { s.sp = true } }
	return s
}

func getProfileCredentials(profile string) (string, string) {
	s := getProfileCredState(profile); return s.user, s.pass
}

func hasProfileCredentials(profile string) bool {
	_, err := os.Stat(filepath.Join(configDir, profile+".cred"))
	return err == nil
}

func saveProfileCredentials(profile, u, p string) { saveProfileCredentialsFull(profile, u, p, true, true, false, false) }

func saveProfileCredentialsFull(profile, u, p string, su, sp, applyAll, saveProf bool) {
	var lines []string
	if u != "" { lines = append(lines, u) } else { lines = append(lines, "") }
	if p != "" { lines = append(lines, p) } else { lines = append(lines, "") }
	if su { lines = append(lines, "su=1") } else { lines = append(lines, "su=0") }
	if sp { lines = append(lines, "sp=1") } else { lines = append(lines, "sp=0") }
	if applyAll { lines = append(lines, "a=1") } else { lines = append(lines, "a=0") }
	if saveProf { lines = append(lines, "f=1") } else { lines = append(lines, "f=0") }
	path := filepath.Join(configDir, profile+".cred")
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
}

func deleteProfileCredentials(profile string) {
	os.Remove(filepath.Join(configDir, profile+".cred"))
}

func getProfilesWithCustomCredentials() []string {
	var list []string
	entries, _ := os.ReadDir(configDir)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cred") && entry.Name() != "session.cred" { list = append(list, strings.TrimSuffix(entry.Name(), ".cred")) }
	}
	return list
}

func clearAllCustomCredentials() {
	entries, _ := os.ReadDir(configDir)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cred") && entry.Name() != "session.cred" { os.Remove(filepath.Join(configDir, entry.Name())) }
	}
}

func refreshProfiles() {
	vpnStateMu.Lock()
	defer vpnStateMu.Unlock()
	
	profileMap = make(map[string]string)
	entries, err := os.ReadDir(configDir)
	if err != nil {
		os.WriteFile("/tmp/rcp-error.log", []byte(fmt.Sprintf("Error reading %s: %v\n", configDir, err)), 0644)
		return
	}
	var newProfiles []string
	seen := make(map[string]bool)
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if !entry.IsDir() && strings.EqualFold(ext, ".ovpn") {
			name := strings.TrimSuffix(entry.Name(), ext)
			if seen[strings.ToLower(name)] { continue }
			seen[strings.ToLower(name)] = true
			
			newProfiles = append(newProfiles, name)
			profileMap[name] = entry.Name()
			os.Chmod(filepath.Join(configDir, entry.Name()), 0644)
		}
	}
	profiles = newProfiles
}

func updateAllStats() {
	refreshProfiles()
	vpnStateMu.Lock()
	defer vpnStateMu.Unlock()
	
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
	go startSmartOrchestrator()
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
	hasAuthUserPass := false
	// NOTE: persist-key and persist-tun are intentionally NOT removed — they are essential
	// for connection resilience when network is interrupted (laptop sleep, brief disconnects).
	// persist-key  → keeps TLS keys in memory so re-auth is not needed after network recovery
	// persist-tun  → keeps TUN interface open so the tunnel can resume without full restart
	reRemove := regexp.MustCompile(`(?i)^(--)?(client-cert-not-required|verify-client-cert|route-method|route-delay)`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") { newLines = append(newLines, line); continue }
		if strings.HasPrefix(strings.ToLower(trimmed), "auth-user-pass") {
			hasAuthUserPass = true
			continue
		}
		if reRemove.MatchString(trimmed) { continue }
		newLines = append(newLines, line)
	}
	if hasAuthUserPass {
		newLines = append(newLines, "auth-user-pass")
	}
	return os.WriteFile(filePath, []byte(strings.Join(newLines, "\n")), 0644)
}

func profileRequiresAuth(profile string) bool {
	ovpnFile := profileMap[profile]
	if ovpnFile == "" {
		return false
	}
	ovpnPath := filepath.Join(configDir, ovpnFile)
	content, err := os.ReadFile(ovpnPath)
	if err != nil {
		return false
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "auth-user-pass") {
			return true
		}
	}
	return false
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
		if !profileRequiresAuth(profile) {
			connectVPN(profile, "", "")
			return
		}
		defU := sharedUser; if defU == "" { defU = username }
		defP := sharedPass
		st := getProfileCredState(profile)
		if st.user != "" { defU = st.user; defP = st.pass }
		res := showLoginWindow(profile, defU, defP, st.su, st.sp, st.applyAll, st.saveProf)
		if !res.canceled {
			if res.applyAll { saveSharedCredentials(res.user, res.password) }
			if res.saveProfile || res.saveUser || res.savePass {
				saveProfileCredentialsFull(profile, res.user, res.password, res.saveUser, res.savePass, res.applyAll, res.saveProfile)
			}
			connectVPN(profile, res.user, res.password)
		}
		return
	}

	if len(os.Args) > 1 && (os.Args[1] == "ui" || os.Args[1] == "mini") {
		if os.Args[1] == "mini" { miniMode = true }
		startBackgroundSync()

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
		systray.Run(onTrayReady, nil)
	}
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

func importProfileTray() {
	script := `POSIX path of (choose file with prompt "Select an OVPN profile" of type {"ovpn", "com.openvpn.ovpn", "org.openvpn.ovpn"})`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil { return }
	src := strings.TrimSpace(string(out))
	if src == "" { return }
	ext := strings.ToLower(filepath.Ext(src))
	if ext != ".ovpn" { return }
	name := strings.TrimSuffix(filepath.Base(src), ext)
	dst := filepath.Join(configDir, name+".ovpn")
	data, err := os.ReadFile(src)
	if err != nil { return }
	os.WriteFile(dst, data, 0644)
	exe, _ := os.Executable()
	appPath := exe
	if strings.Contains(exe, ".app/Contents/MacOS/") { appPath = exe[:strings.Index(exe, ".app/")+4] }
	exec.Command("open", "-n", appPath).Start()
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
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

func measureLatency(profile string) float64 {
	ip := getVPNIPFromLog(profile)
	if ip == "" { return 9999 }
	iface := findInterfaceByIP(ip)
	if iface == "" { return 9999 }

	targets := []string{"1.1.1.1", "8.8.8.8"}
	var bestLat float64 = 9999

	for _, target := range targets {
		// Ping through specific interface
		cmd := exec.Command("ping", "-c", "1", "-W", "800", "-S", ip, target)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		out, err := cmd.Output()
		if err != nil { continue }

		re := regexp.MustCompile(`time=([\d.]+) ms`)
		match := re.FindStringSubmatch(string(out))
		if len(match) > 1 {
			l, _ := strconv.ParseFloat(match[1], 64)
			if l < bestLat { bestLat = l }
		}
	}
	return bestLat
}

func flushDNS() {
	// macOS DNS Flush
	exec.Command("sudo", "dscacheutil", "-flushcache").Run()
	exec.Command("sudo", "killall", "-HUP", "mDNSResponder").Run()
}

func getActualActiveInterface() string {
	// We check who handles traffic to a common internet IP
	cmd := exec.Command("route", "get", "1.1.1.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	if err != nil { return "" }
	
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "interface:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

func probeHostThroughProfile(profile string, targetIP string) (float64, error) {
	ip := getVPNIPFromLog(profile)
	if ip == "" { return 0, fmt.Errorf("no VPN IP") }
	
	start := time.Now()
	
	// Use TCP Dialing instead of just Ping for better accuracy with web services
	// We force the dial through the specific VPN local IP
	localAddr, _ := net.ResolveTCPAddr("tcp", ip+":0")
	dialer := net.Dialer{
		LocalAddr: localAddr,
		Timeout:   1500 * time.Millisecond,
	}
	
	conn, err := dialer.Dial("tcp", targetIP+":443")
	if err == nil {
		conn.Close()
		return float64(time.Since(start).Milliseconds()), nil
	}
	
	// Fallback to port 80
	conn, err = dialer.Dial("tcp", targetIP+":80")
	if err == nil {
		conn.Close()
		return float64(time.Since(start).Milliseconds()), nil
	}

	// Final fallback to Ping if TCP fails (some internal resources might only respond to ICMP)
	cmd := exec.Command("ping", "-c", "1", "-W", "1000", "-S", ip, targetIP)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Run()
	if err == nil {
		lat := float64(time.Since(start).Milliseconds())
		return lat, nil
	}

	fmt.Printf("   [DEBUG] %-10s -> %s: UNREACHABLE (TCP & Ping Failed)\n", profile, targetIP)
	return 0, fmt.Errorf("unreachable")
}

func probeHostHTTPStatus(profile string, targetIP string) (int, error) {
	ip := getVPNIPFromLog(profile)
	if ip == "" { return 0, fmt.Errorf("no VPN IP") }

	localAddr, _ := net.ResolveTCPAddr("tcp", ip+":0")
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			LocalAddr: localAddr,
			Timeout:   2500 * time.Millisecond,
		}).DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   3000 * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Try to get hostname from system DNS cache for better accuracy
	hostname := ""
	out, err := exec.Command("dscacheutil", "-q", "host", "-a", "ip", targetIP).Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "name:") {
				hostname = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "name:"))
				break
			}
		}
	}
	
	// Fallback to Reverse DNS
	if hostname == "" {
		names, _ := net.LookupAddr(targetIP)
		if len(names) > 0 {
			hostname = strings.TrimSuffix(names[0], ".")
		}
	}

	doGet := func(proto, host string) (int, error) {
		req, _ := http.NewRequest("GET", proto+"://"+targetIP, nil)
		if host != "" { req.Host = host }
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			return resp.StatusCode, nil
		}
		return 0, err
	}

	// 1. Try with discovered hostname
	status, err := doGet("https", hostname)
	if err == nil && status < 400 { return status, nil }

	// 2. If 403/404, try with common suffixes if hostname is just a short name
	if (status == 403 || status == 404 || hostname == "") && (strings.HasPrefix(targetIP, "10.") || strings.HasPrefix(targetIP, "172.")) {
		suffixes := []string{"sicepat.tech", "sicepat.lan", "sicepat.id"}
		for _, s := range suffixes {
			h := hostname; if h == "" { h = "server" }
			if !strings.Contains(h, ".") { h = h + "." + s }
			st, err := doGet("https", h)
			if err == nil && st < 400 { return st, nil }
		}
	}

	// 3. Last resort: Try IP directly (HTTPS then HTTP)
	if status, err := doGet("https", ""); err == nil { return status, nil }
	if status, err := doGet("http", ""); err == nil { return status, nil }

	return 0, fmt.Errorf("probe failed")
}

func applyHostRoute(targetIP string, profile string) error {
	ip := getVPNIPFromLog(profile)
	iface := findInterfaceByIP(ip)
	if iface == "" { return fmt.Errorf("interface not found") }

	// Removed the direct log here to avoid premature logging before confirmation

	// First, remove existing route to avoid "File exists"
	exec.Command("sudo", "route", "delete", "-host", targetIP).Run()
	
	// Add the new specific route
	cmd := exec.Command("sudo", "route", "add", "-host", targetIP, "-interface", iface)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to add route: %v", err)
	}

	// Force flush socket states for this IP to kill "stuck" browser connections
	exec.Command("sudo", "pfctl", "-k", targetIP).Run()

	return nil
}

func triggerReoptimization() {
	go func() {
		// Clear host route cache and system routes to force fresh sequential testing
		hostRouteCacheMu.Lock()
		for _, entry := range hostRouteCache {
			exec.Command("sudo", "route", "delete", "-host", entry.IP).Run()
		}
		hostRouteCache = make(map[string]HostRouteEntry)
		hostRouteCacheMu.Unlock()

		orchestrateOnce()
		flushDNS()
		addAuditLog("Manual Re-optimization: All smart routes reset", "info")
	}()
}

func orchestrateOnce() {
	refreshProfiles()
	var active []string
	for _, p := range profiles {
		if isProfileConnected(p) { active = append(active, p) }
	}

	if len(active) == 0 { return }

	results := make(map[string]float64)
	var bestProfile string
	minLatency := 9999.0

	for _, p := range active {
		lat := measureLatency(p)
		results[p] = lat
		if lat < minLatency {
			minLatency = lat
			bestProfile = p
		}
	}

	globalLatenciesMu.Lock()
	globalLatencies = results
	globalLatenciesMu.Unlock()

	if bestProfile == "" || minLatency >= 9999 { return }

	orchestratorMu.Lock()
	defer orchestratorMu.Unlock()

	// Anti-flapping logic: 
	// Only switch if the new path is significantly better (15% threshold)
	// or if the current winner is no longer available/stable.
	shouldSwitch := false
	if lastWinner == "" || lastWinner != bestProfile {
		if lastWinner == "" {
			shouldSwitch = true
		} else {
			currentWinnerLat := results[lastWinner]
			// Switch if current winner is dead or new winner is 15% better
			if currentWinnerLat >= 9999 || minLatency < (currentWinnerLat * 0.85) {
				shouldSwitch = true
			}
		}
	}

	if shouldSwitch {
		oldWinner := lastWinner
		applySmartRouting(active, bestProfile)
		lastWinner = bestProfile
		lastWinnerLatency = minLatency
		flushDNS()

		if oldWinner == "" {
			addAuditLog(fmt.Sprintf("Primary route established: %s (%.1fms)", bestProfile, minLatency), "success")
		} else {
			addAuditLog(fmt.Sprintf("Optimized: %s -> %s (%.1fms vs %.1fms)", oldWinner, bestProfile, minLatency, results[oldWinner]), "success")
		}
	}

	// Update current active interface for UI indicators
	actual := getActualActiveInterface()
	currentActiveIfaceMu.Lock()
	currentActiveInterface = actual
	currentActiveIfaceMu.Unlock()
	
	// Check for mismatch
	if lastWinner != "" {
		ip := getVPNIPFromLog(lastWinner)
		winnerIface := findInterfaceByIP(ip)
		if actual != "" && winnerIface != "" && actual != winnerIface {
			addAuditLog(fmt.Sprintf("Routing Mismatch: System using %s instead of %s", actual, winnerIface), "warning")
		}
	}

	// === DYNAMIC TRAFFIC MONITORING ===
	// Already started in startSmartOrchestrator or main

	// Cache Cleanup: Remove entries if the profile is no longer active
	hostRouteCacheMu.Lock()
	defer hostRouteCacheMu.Unlock()
	for host, entry := range hostRouteCache {
		if !isProfileConnected(entry.Profile) {
			delete(hostRouteCache, host)
			// Also clean up the system route
			exec.Command("sudo", "route", "delete", "-host", entry.IP).Run()
			addAuditLog(fmt.Sprintf("Cache Cleaned: Removed route for %s (Profile %s disconnected)", host, entry.Profile), "info")
		}
	}
}

func monitorAndProbeTraffic() {
	// Scan active connections using netstat
	out, err := exec.Command("netstat", "-n", "-f", "inet").Output()
	if err != nil { return }

	// Refresh profiles once before the loop
	refreshProfiles()
	
	vpnStateMu.Lock()
	activeVPNs := []string{}
	for _, p := range profiles {
		if isProfileConnected(p) {
			activeVPNs = append(activeVPNs, p)
		}
	}
	vpnStateMu.Unlock()

	if len(activeVPNs) < 2 { return }

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "ESTABLISHED") { continue }
		
		fields := strings.Fields(line)
		if len(fields) < 5 { continue }
		
		foreignAddr := fields[4] // Destination IP.Port
		parts := strings.Split(foreignAddr, ".")

		// FILTER: Only concern with Browser Traffic (Port 80 and 443)
		isBrowser := strings.HasSuffix(foreignAddr, ".443") || strings.HasSuffix(foreignAddr, ".80") || 
					 strings.HasSuffix(foreignAddr, ":443") || strings.HasSuffix(foreignAddr, ":80")
		
		if !isBrowser { continue }

		targetIP := ""
		if len(parts) >= 4 {
			if strings.Contains(foreignAddr, ":") {
				targetIP = strings.Split(foreignAddr, ":")[0]
			} else {
				targetIP = strings.Join(parts[0:4], ".")
			}
		}
		
		if targetIP == "" { continue }
		
		if strings.HasPrefix(targetIP, "127.") || strings.HasPrefix(targetIP, "192.168.") || strings.HasPrefix(targetIP, "0.") {
			continue
		}

		hostRouteCacheMu.Lock()
		entry, exists := hostRouteCache[targetIP]
		hostRouteCacheMu.Unlock()

		// SMART RE-VALIDATION LOGIC:
		// 1. If host not in cache -> Probe immediately.
		// 2. If host marked "NONE" -> Retry probe (maybe a VPN just connected).
		// 3. If route is older than 30 minutes -> Re-probe to ensure it's still optimal.
		shouldProbe := !exists || entry.Profile == "NONE" || time.Since(entry.Timestamp) > 30*time.Minute

		if shouldProbe {
			// Quietly probe in the background
			go func(ip string, active []string) {
				type resEntry struct {
					profile string
					latency float64
					status  int
				}
				var results []resEntry
				
				// SEQUENTIAL ISOLATED PROBING
				for _, p := range active {
					ipAddr := getVPNIPFromLog(p)
					iface := findInterfaceByIP(ipAddr)
					if iface == "" { continue }

					// 1. Add temporary route for this probe
					exec.Command("sudo", "route", "delete", "-host", ip).Run()
					exec.Command("sudo", "route", "add", "-host", ip, "-interface", iface).Run()
					
					// Settle delay for OS routing table
					time.Sleep(150 * time.Millisecond)

					// 2. Perform isolated probe
					lat, err := probeHostThroughProfile(p, ip)
					status := 0
					if err == nil {
						status, _ = probeHostHTTPStatus(p, ip)
					}
					results = append(results, resEntry{p, lat, status})

					// 3. Cleanup temporary route
					exec.Command("sudo", "route", "delete", "-host", ip).Run()
					exec.Command("sudo", "pfctl", "-k", ip).Run() // Flush state for this specific IP
				}

				bestLat := 9999.0
				bestProfile := ""
				
				// STRICT: Priority 1: Profiles that give 200 OK
				for _, r := range results {
					if r.status == 200 {
						isProd := strings.Contains(strings.ToLower(r.profile), "prod")
						if bestProfile == "" || (isProd && !strings.Contains(strings.ToLower(bestProfile), "prod")) || (isProd == strings.Contains(strings.ToLower(bestProfile), "prod") && r.latency < bestLat) {
							bestLat = r.latency
							bestProfile = r.profile
						}
					}
				}

				// If NO 200 OK found, we check if one is unambiguously "better" (e.g. 201, 204, or 403 on prod)
				if bestProfile == "" {
					for _, r := range results {
						if r.status > 200 && r.status < 300 {
							bestProfile = r.profile; bestLat = r.latency; break
						}
					}
				}

				// AMBIGUITY CHECK: If still no profile, DO NOT force route.
				if bestProfile == "" {
					hostRouteCacheMu.Lock()
					// Negative cache for a shorter time to allow retries
					hostRouteCache[ip] = HostRouteEntry{IP: ip, Profile: "NONE", Timestamp: time.Now()}
					hostRouteCacheMu.Unlock()
					return 
				}

				if bestProfile != "" {
					fmt.Printf("\033[32m[ASSIGN] Routing %s to %s\033[0m\n", ip, bestProfile)
					applyHostRoute(ip, bestProfile)
					hostRouteCacheMu.Lock()
					hostRouteCache[ip] = HostRouteEntry{IP: ip, Profile: bestProfile, Timestamp: time.Now()}
					hostRouteCacheMu.Unlock()
					addAuditLog(fmt.Sprintf("Auto-Discovered: %s -> %s", ip, bestProfile), "success")
					fmt.Printf("\033[32m[SMART-ROUTE] %s is now handled by %s (%.1fms)\033[0m\n", ip, bestProfile, bestLat)
				}
			}(targetIP, activeVPNs)
		}
	}
}

func startSmartOrchestrator() {
	// Start background traffic monitoring
	go func() {
		for {
			monitorAndProbeTraffic()
			time.Sleep(5 * time.Second)
		}
	}()

	time.Sleep(5 * time.Second)
	for {
		orchestrateOnce()
		
		// Adjust sleep based on activity
		sleepTime := 30 * time.Second
		activeCount := 0
		for _, p := range profiles { if isProfileConnected(p) { activeCount++ } }
		if activeCount > 1 { sleepTime = 10 * time.Second }
		
		time.Sleep(sleepTime)
	}
}

func applySmartRouting(profiles []string, winner string) {
	// On macOS, OpenVPN often uses --redirect-gateway def1 which adds
	// 0.0.0.0/1 and 128.0.0.0/1 routes. We need to prioritize these.
	
	for i, p := range profiles {
		metric := 100 + (i * 10)
		if p == winner { metric = 40 } // Winner gets highest priority
		
		ip := getVPNIPFromLog(p)
		iface := findInterfaceByIP(ip)
		if iface == "" { continue }

		// Change metric for the specific interface's default routes.
		// We target 'default', '0.0.0.0/1', and '128.0.0.0/1'.
		destinations := []string{"default", "0.0.0.0/1", "128.0.0.0/1"}
		
		for _, dest := range destinations {
			// We use 'route change' which is standard on macOS for modifying existing routes
			// If it fails, it usually means the route doesn't exist for that destination/interface combo
			exec.Command("sudo", "route", "change", "-net", dest, "-interface", iface, "-metric", strconv.Itoa(metric)).Run()
			
			// Also try without -net for 'default'
			if dest == "default" {
				exec.Command("sudo", "route", "change", "default", "-interface", iface, "-metric", strconv.Itoa(metric)).Run()
			}
		}
	}
	
	// Add a small delay to let the OS apply routing changes
	time.Sleep(500 * time.Millisecond)
}

func connectVPN(profile string, user string, password string) error {
	ovpnFile := profileMap[profile]
	ovpnPath := filepath.Join(configDir, ovpnFile)
	sanitizeOvpn(ovpnPath)
	logPath := filepath.Join(configDir, profile+".log")
	pidPath := filepath.Join(configDir, profile+".pid")
	os.Remove(logPath); os.Remove(pidPath)
	os.WriteFile(logPath, []byte(""), 0666)

	requiresAuth := profileRequiresAuth(profile)
	var tmpAuthPath string
	if requiresAuth {
		tmpAuthPath = filepath.Join(configDir, ".tmp_auth_"+profile)
		os.WriteFile(tmpAuthPath, []byte(fmt.Sprintf("%s\n%s", user, password)), 0600)
	}

	args := []string{openvpnBin, "--config", ovpnPath}
	if requiresAuth {
		args = append(args, "--auth-user-pass", tmpAuthPath)
	}
	args = append(args,
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

	cmd := exec.Command("sudo", args...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		if requiresAuth {
			os.Remove(tmpAuthPath)
		}
		return fmt.Errorf("OpenVPN Start Error: %v", err)
	}

	// Wait and check if it survives or fails auth
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if !isProfileConnected(profile) {
			logContent, _ := os.ReadFile(logPath)
			if requiresAuth && strings.Contains(string(logContent), "AUTH_FAILED") {
				os.Remove(tmpAuthPath)
				return fmt.Errorf("AUTHENTICATION FAILED")
			}
			if requiresAuth {
				os.Remove(tmpAuthPath)
			}
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
	if requiresAuth {
		go func() { time.Sleep(15 * time.Second); os.Remove(tmpAuthPath) }()
	}
	return nil
}

// vpnWatchdog monitors a connected profile and auto-reconnects if it drops unexpectedly.
// It stops when the profile is no longer in activeConnections (i.e., user disconnected).
func vpnWatchdog(profile string) {
	// Grace period before first check
	time.Sleep(10 * time.Second)
	for {
		activeConnMu.Lock()
		_, shouldWatch := activeConnections[profile]
		activeConnMu.Unlock()

		if !shouldWatch {
			// User explicitly disconnected — stop watchdog
			return
		}

		if !isProfileConnected(profile) || measureLatency(profile) > 5000 {
			// Connection dropped OR tunnel is unresponsive (latency > 5s or RTO)
			// Connection dropped unexpectedly — attempt reconnect
			// Brief wait to let network settle (e.g. after wake from sleep)
			time.Sleep(5 * time.Second)

			activeConnMu.Lock()
			cred, stillActive := activeConnections[profile]
			activeConnMu.Unlock()
			if !stillActive { return }

			// If it was a false connection (process alive but tunnel dead), kill it first
			if isProfileConnected(profile) {
				disconnectVPN(profile)
				time.Sleep(1 * time.Second)
				activeConnMu.Lock()
				activeConnections[profile] = cred // restore cred after disconnectVPN deleted it
				activeConnMu.Unlock()
			}

			// Try to reconnect (ignore errors — watchdog will retry next cycle)
			_ = connectVPNDirect(profile, cred.user, cred.pass)
		}

		time.Sleep(30 * time.Second)
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

	requiresAuth := profileRequiresAuth(profile)
	var tmpAuthPath string
	if requiresAuth {
		tmpAuthPath = filepath.Join(configDir, ".tmp_auth_"+profile)
		os.WriteFile(tmpAuthPath, []byte(fmt.Sprintf("%s\n%s", user, password)), 0600)
	}

	args := []string{openvpnBin, "--config", ovpnPath}
	if requiresAuth {
		args = append(args, "--auth-user-pass", tmpAuthPath)
	}
	args = append(args,
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

	cmd := exec.Command("sudo", args...)
	_, err := cmd.CombinedOutput()
	if requiresAuth {
		go func() { time.Sleep(15 * time.Second); os.Remove(tmpAuthPath) }()
	}
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

func onTrayReady() {
	systray.SetIcon(trayIconData)
	systray.SetTitle("")
	mOpenUI := systray.AddMenuItem("Open Terminal UI", "")
	systray.AddSeparator()
	trayItems := make(map[string]*trayProfileItem)
	for _, p := range profiles {
		parent := systray.AddMenuItem(p, "")
		connect := parent.AddSubMenuItem("Connect", "")
		del := parent.AddSubMenuItem("Delete", "")
		trayItems[p] = &trayProfileItem{parent: parent, connect: connect, delete: del}
	}
	systray.AddSeparator()
	mImport := systray.AddMenuItem("Import OVPN Profile", "")
	systray.AddSeparator()
	mLastEvent := systray.AddMenuItem("Ready", "")
	mLastEvent.Disable()
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")

	updateTray := func() {
		activeCount := 0
		for _, p := range profiles {
			connected := isProfileConnected(p)
			title := p
			ti, ok := trayItems[p]; if !ok { continue }
			if connected {
				activeCount++
				ti.connect.SetTitle("Disconnect")
				orchestratorMu.Lock()
				isPrimary := (p == lastWinner)
				orchestratorMu.Unlock()
				ip := getVPNIPFromLog(p)
				iface := findInterfaceByIP(ip)
				currentActiveIfaceMu.Lock()
				isActive := (iface != "" && iface == currentActiveInterface)
				currentActiveIfaceMu.Unlock()
				isMoving := false
				if s, ok := globalNetStats[p]; ok { if s.InSpeed > 1024 || s.OutSpeed > 512 { isMoving = true } }
				title = "\u25CF " + title
				if isPrimary { title += " [PRIMARY]" }
				if isActive { title += " [ACTIVE]" }
				if isMoving { title += " [TRANS]" }
			} else {
				ti.connect.SetTitle("Connect")
				title = "\u25CB " + title
			}
			ti.parent.SetTitle(title)
		}
		auditMu.Lock()
		if len(auditLogs) > 0 { mLastEvent.SetTitle("Event: " + auditLogs[len(auditLogs)-1].Message) }
		auditMu.Unlock()
		if activeCount > 0 { systray.SetTitle(fmt.Sprintf(" %d", activeCount)) } else { systray.SetTitle("") }
	}
	for _, p := range profiles {
		go func(p string, ti *trayProfileItem) {
			for {
				select {
				case <-ti.connect.ClickedCh:
					if isProfileConnected(p) { disconnectVPN(p) } else if profileRequiresAuth(p) { openLoginWindowSubprocess(p) } else { connectVPN(p, "", "") }
				case <-ti.delete.ClickedCh:
					script := fmt.Sprintf(`display dialog "Delete profile '%s'?\n\nThis will remove the .ovpn file, credentials, and logs." buttons {"Cancel", "Delete"} default button "Cancel" with icon caution`, p)
					out, _ := exec.Command("osascript", "-e", script).Output()
					if !strings.Contains(string(out), "Delete") { continue }
					disconnectVPN(p)
					os.Remove(filepath.Join(configDir, p+".ovpn"))
					os.Remove(filepath.Join(configDir, p+".pid"))
					os.Remove(filepath.Join(configDir, p+".log"))
					os.Remove(filepath.Join(configDir, p+".cred"))
					refreshProfiles()
					ti.parent.Hide()
				}
				updateTray()
			}
		}(p, trayItems[p])
	}
	go func() { for { select { case <-mOpenUI.ClickedCh: openTUI(); case <-mImport.ClickedCh: importProfileTray(); case <-mQuit.ClickedCh: systray.Quit() } } }()
	go func() { for { time.Sleep(3 * time.Second); updateTray() } }()
}


const (
	stateSelect = iota
	stateConnUser
	stateConnPass
	stateConnecting
	stateError
	stateConfirmDelete
	stateRenameImport
	stateUsername
	stateConfirmOverwrite
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
	saveMode   int // 0 = apply_all, 1 = save_profile, 2 = none
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
		// Keep choices in sync with global profiles (handles dynamic imports/deletes/renames)
		m.choices = profiles
		if m.cursor >= len(m.choices) && len(m.choices) > 0 {
			m.cursor = len(m.choices) - 1
		}
		for _, p := range profiles {
			m.connected[p] = isProfileConnected(p)
			if m.connected[p] { m.ips[p] = getVPNIPFromLog(p) }
			if _, ok := m.offsets[p]; !ok { m.offsets[p] = rand.Intn(100) }
		}
		return m, tea.Tick(time.Millisecond*80, func(t time.Time) tea.Msg { return t })
	case tea.KeyMsg:
		if m.state == stateConnecting { return m, nil }
		keyStr := msg.String()
		isInputState := (m.state == stateConnUser || m.state == stateConnPass || m.state == stateUsername || m.state == stateRenameImport)
		
		if isInputState {
			switch keyStr {
			case "ctrl+c":
				closeTerminal()
				return m, tea.Quit
			case "esc":
				m.state = stateSelect
				m.statusMsg = ""
				return m, nil
			case "tab":
				if m.state == stateConnUser || m.state == stateConnPass {
					m.saveMode = (m.saveMode + 1) % 3
				}
				return m, nil
			case "enter":
				if m.state == stateConnUser {
					m.tempUser = m.input.Value()
					m.state = stateConnPass; m.input.EchoMode = textinput.EchoPassword
					m.input.Prompt = ""; m.input.SetValue(""); m.input.Focus()
				} else if m.state == stateConnPass {
					m.tempPass = m.input.Value()
					if m.tempPass == "" {
						if m.saveMode == 1 && hasProfileCredentials(m.choices[m.cursor]) {
							_, m.tempPass = getProfileCredentials(m.choices[m.cursor])
						} else if m.saveMode == 0 && sharedPass != "" {
							m.tempPass = sharedPass
						}
					}
					p := m.choices[m.cursor]
					if m.saveMode == 0 {
						customList := getProfilesWithCustomCredentials()
						if len(customList) > 0 {
							m.state = stateConfirmOverwrite
							return m, nil
						}
						saveSharedCredentials(m.tempUser, m.tempPass)
						deleteProfileCredentials(p)
					} else if m.saveMode == 1 {
						saveProfileCredentials(p, m.tempUser, m.tempPass)
					} else {
						deleteProfileCredentials(p)
					}
					m.state = stateConnecting
					return m, func() tea.Msg { if err := connectVPN(p, m.tempUser, m.tempPass); err != nil { return err }; return p }
				} else if m.state == stateRenameImport {
					newName := strings.TrimSpace(m.input.Value())
					if newName != "" {
						newName = strings.TrimSuffix(newName, ".ovpn"); dst := filepath.Join(configDir, newName+".ovpn")
						exec.Command("cp", m.importPath, dst).Run()
						sanitizeOvpn(dst)
						m.state = stateSelect
					}
				} else if m.state == stateUsername {
					saveUsername(m.input.Value()); m.state = stateSelect
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		switch keyStr {
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
		case "x": 
			if len(m.choices) > 0 && (m.state == stateSelect || m.state == stateError) { 
				m.state = stateConfirmDelete 
			}
		case "y":
			if m.state == stateConfirmDelete {
				p := m.choices[m.cursor]
				disconnectVPN(p) // also stops watchdog
				deleteProfileCredentials(p)
				os.Remove(filepath.Join(configDir, profileMap[p]))
				m.state = stateSelect; if m.cursor >= len(m.choices) && m.cursor > 0 { m.cursor-- }
			} else if m.state == stateConfirmOverwrite {
				clearAllCustomCredentials()
				saveSharedCredentials(m.tempUser, m.tempPass)
				m.state = stateConnecting; p := m.choices[m.cursor]
				return m, func() tea.Msg { if err := connectVPN(p, m.tempUser, m.tempPass); err != nil { return err }; return p }
			}
		case "n":
			if m.state == stateConfirmOverwrite {
				saveSharedCredentials(m.tempUser, m.tempPass)
				m.state = stateConnecting; p := m.choices[m.cursor]
				return m, func() tea.Msg { if err := connectVPN(p, m.tempUser, m.tempPass); err != nil { return err }; return p }
			} else if m.state == stateConfirmDelete {
				m.state = stateSelect
			}
		case "esc":
			m.state = stateSelect; m.statusMsg = ""
		case "enter":
			if m.state == stateSelect || m.state == stateError {
				if len(m.choices) == 0 { return m, nil }
				p := m.choices[m.cursor]
				if isProfileConnected(p) {
					disconnectVPN(p) // stops watchdog + kills process
				} else { 
					if !profileRequiresAuth(p) {
						m.state = stateConnecting
						return m, func() tea.Msg { if err := connectVPN(p, "", ""); err != nil { return err }; return p }
					}
					m.state = stateConnUser; m.input.EchoMode = textinput.EchoNormal
					m.input.Prompt = ""; val := sharedUser; if val == "" { val = username }
					if hasProfileCredentials(p) {
						val, _ = getProfileCredentials(p)
						m.saveMode = 1
					} else if sharedPass != "" {
						m.saveMode = 0
					} else {
						m.saveMode = 2
					}
					m.input.SetValue(val); m.input.Focus()
					m.input.SetCursor(len(val))
				}
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
				activeLines = append(activeLines, fmt.Sprintf("⚡ %-10s (%-15s) %s", name, ip, scanner))
			}
		}
		if len(activeLines) == 0 { return miniBoxStyle.Render("RCP: No Active VPN") + "\n" }
		return miniBoxStyle.Render(strings.Join(activeLines, "\n")) + "\n"
	}
	var body string
	
	header := lipgloss.JoinHorizontal(lipgloss.Center,
		titleStyle.Render(" RCP "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5CF1")).Padding(0, 1).Render("Light V2.0.0"),
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
					statusIcon = "⚡"
					ip := getVPNIPFromLog(p); if ip != "" { ipInfo = " (" + ip + ")" }
					scanner = getMiniScanner(m.frame + m.offsets[p], 12)
					if s, ok := globalNetStats[p]; ok {
						primaryStr := ""
						orchestratorMu.Lock()
						if p == lastWinner && connected { primaryStr = " [PRIMARY]" }
						orchestratorMu.Unlock()

						activeStr := ""
						ip := getVPNIPFromLog(p)
						iface := findInterfaceByIP(ip)
						currentActiveIfaceMu.Lock()
						if iface != "" && iface == currentActiveInterface { activeStr = " [ACTIVE]" }
						currentActiveIfaceMu.Unlock()

						activityStr := ""
						if s.InSpeed > 1024 || s.OutSpeed > 512 { activityStr = " [TRANS]" }
						speedInfo = fmt.Sprintf(" %s %s %s %s %s %s %s", downStyle.Render("↓"), formatSpeed(s.InSpeed), upStyle.Render("↑"), formatSpeed(s.OutSpeed), primaryStr, activeStr, activityStr)
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

		auditMu.Lock()
		if len(auditLogs) > 0 {
			body += "\n" + dimStyle.Render("  "+auditLogs[len(auditLogs)-1].Message)
		}
		auditMu.Unlock()
	} else if m.state == stateConnecting {
		body = fmt.Sprintf("Connecting to %s...\n\nVerifying connection...", m.choices[m.cursor])
	} else if m.state == stateUsername {
		body = fmt.Sprintf("Edit Username:\n\n%s\n\n[Enter] Save   [Esc] Cancel", m.input.View())
	} else if m.state == stateConnUser || m.state == stateConnPass {
		userDisplay := m.tempUser; if m.state == stateConnUser { userDisplay = m.input.View() }
		passDisplay := strings.Repeat("*", len(m.tempPass)); if m.state == stateConnPass { passDisplay = m.input.View() }
		var modeStr string
		switch m.saveMode {
		case 0:
			modeStr = brightStyle.Render("Apply for all profiles")
		case 1:
			modeStr = brightStyle.Render("Save for this profile only")
		default:
			modeStr = brightStyle.Render("Don't save")
		}
		body = fmt.Sprintf("Profile: %s\n\nUsername: %s\nPassword: %s\n\nSave Mode: %s [Tab to Change]\n\n[Enter] Continue", 
			brightStyle.Render(m.choices[m.cursor]), userDisplay, passDisplay, modeStr)
	} else if m.state == stateConfirmOverwrite {
		customList := getProfilesWithCustomCredentials()
		var listLines []string
		for _, p := range customList {
			listLines = append(listLines, "  • "+p)
		}
		body = fmt.Sprintf("Warning: The following profiles have custom credentials:\n%s\n\nOverwrite all custom credentials?\n\n[y] Overwrite All\n[n] Apply to others only (Keep custom)\n[Esc] Cancel", 
			strings.Join(listLines, "\n"))
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
