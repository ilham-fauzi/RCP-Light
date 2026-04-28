package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

void focus_window(void *w) {
    NSWindow *window = (NSWindow *)w;
    [NSApp activateIgnoringOtherApps:YES];
    [window makeKeyAndOrderFront:nil];
    [window orderFrontRegardless];
}

void make_dashboard_frameless(void *w) {
    NSWindow *window = (NSWindow *)w;
    [window setStyleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskFullSizeContentView];
    [window setTitleVisibility:NSWindowTitleHidden];
    [window setTitlebarAppearsTransparent:YES];
    [window setHasShadow:YES];
    [window setBackgroundColor:[NSColor colorWithRed:0.04 green:0.05 blue:0.06 alpha:1.0]];
    [window setMovableByWindowBackground:YES];
    [window setLevel:NSStatusWindowLevel]; // Show above other windows but below system UI
    
    // Position near tray icon
    NSPoint mouseLoc = [NSEvent mouseLocation];
    NSScreen *screen = [NSScreen mainScreen];
    for (NSScreen *s in [NSScreen screens]) {
        if (NSPointInRect(mouseLoc, [s frame])) {
            screen = s;
            break;
        }
    }
    NSRect visibleFrame = [screen visibleFrame];
    NSRect windowFrame = [window frame];
    
    CGFloat x = mouseLoc.x - (windowFrame.size.width / 2);
    if (x < visibleFrame.origin.x) x = visibleFrame.origin.x + 10;
    if (x + windowFrame.size.width > visibleFrame.origin.x + visibleFrame.size.width)
        x = visibleFrame.origin.x + visibleFrame.size.width - windowFrame.size.width - 10;
        
    CGFloat y = visibleFrame.origin.y + visibleFrame.size.height - windowFrame.size.height - 5;
    [window setFrameOrigin:NSMakePoint(x, y)];
}
*/
import "C"
import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/webview/webview_go"
)

type Dashboard struct {
	w webview.WebView
}

func (d *Dashboard) Run() {
	// Re-initialize config just in case this is a fresh process
	initConfig()

	d.w = webview.New(false)
	defer d.w.Destroy()

	C.make_dashboard_frameless(d.w.Window())

	d.w.SetTitle("RCP Light")
	d.w.SetSize(450, 680, webview.HintFixed)

	// Bindings
	d.w.Bind("getVpnData", func() map[string]interface{} {
		refreshProfiles()
		loadSharedCredentials()
		
		stats := make(map[string]interface{})
		for _, p := range profiles {
			connected := isProfileConnected(p)
			var ip, down, up string
			if connected {
				ip = getVPNIPFromLog(p)
				if s, ok := globalNetStats[p]; ok {
					down = formatSpeed(s.InSpeed)
					up = formatSpeed(s.OutSpeed)
				}
			}
			stats[p] = map[string]interface{}{
				"connected": connected,
				"ip":        ip,
				"down":      down,
				"up":        up,
			}
		}

		return map[string]interface{}{
			"profiles":   profiles,
			"stats":      stats,
			"configDir":  configDir,
			"username":   username,
			"sharedUser": sharedUser,
			"sharedPass": sharedPass,
			"iconBase64": base64.StdEncoding.EncodeToString(iconData),
		}
	})

	d.w.Bind("saveCredentials", func(newUsername string, newPassword string) {
		saveUsername(newUsername)
		saveSharedCredentials(newUsername, newPassword)
	})

	d.w.Bind("exitApp", func() {
		d.w.Terminate()
	})

	d.w.Bind("connect", func(profile string, user string, password string) string {
		C.focus_window(d.w.Window())
		// Small delay to allow macOS to finish window transition before sudo prompt
		time.Sleep(200 * time.Millisecond)
		err := connectVPN(profile, user, password)
		if err != nil { return err.Error() }
		return ""
	})

	d.w.Bind("disconnect", func(profile string) {
		C.focus_window(d.w.Window())
		time.Sleep(200 * time.Millisecond)
		disconnectVPN(profile) // stops watchdog + kills openvpn process
	})

	d.w.Bind("importProfile", func() map[string]string {
		script := `POSIX path of (choose file with prompt "Select an OVPN profile" of type {"ovpn", "com.openvpn.ovpn", "org.openvpn.ovpn"})`
		out, err := exec.Command("osascript", "-e", script).Output()
		if err == nil {
			src := strings.TrimSpace(string(out))
			if src != "" {
				ext := strings.ToLower(filepath.Ext(src))
				if ext == ".ovpn" {
					return map[string]string{"path": src, "name": strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))}
				}
			}
		}
		return nil
	})

	d.w.Bind("saveImport", func(srcPath string, newName string) {
		dst := filepath.Join(configDir, strings.TrimSpace(newName)+".ovpn")
		exec.Command("cp", srcPath, dst).Run()
		refreshProfiles()
	})

	d.w.Bind("deleteProfile", func(profile string) {
		os.Remove(filepath.Join(configDir, profile+".ovpn"))
		refreshProfiles()
	})

	d.w.SetHtml(d.getHTML())
	C.focus_window(d.w.Window())
	d.w.Run()
}

func (d *Dashboard) getHTML() string {
	return `
<!DOCTYPE html>
<html class="dark" lang="en">
<head>
    <meta charset="utf-8"/>
    <meta content="width=device-width, initial-scale=1.0" name="viewport"/>
    <title>RCP Light</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <link href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&family=Material+Symbols+Rounded:wght,FILL@100..700,0..1&display=swap" rel="stylesheet"/>
    <script>
        tailwind.config = {
            darkMode: "class",
            theme: {
                extend: {
                    colors: {
                        surface: "#080a0c",
                        sidebar: "#0c1116",
                        panel: "#12181f",
                        primary: "#5F5CF1",
                        accent: "#00D7FF",
                        success: "#00FFCC",
                        muted: "#64748b",
                    },
                    fontFamily: {
                        sans: ['Plus Jakarta Sans', 'sans-serif'],
                        mono: ['JetBrains Mono', 'monospace'],
                    }
                }
            }
        }
    </script>
    <style>
        body { background-color: #080a0c; color: #e2e8f0; }
        .glass { background: rgba(18, 24, 31, 0.6); backdrop-filter: blur(10px); border: 1px solid rgba(255, 255, 255, 0.05); }
        .sidebar-item.active { background: rgba(95, 92, 241, 0.1); border-right: 2px solid #5F5CF1; color: #5F5CF1; }
        .sidebar-item.active .material-symbols-rounded { font-variation-settings: 'FILL' 1; }
        .node-card { transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1); }
        .node-card:hover { transform: translateY(-2px); border-color: rgba(0, 215, 255, 0.3); background: rgba(18, 24, 31, 0.8); }
        .node-card.connected { border-color: rgba(0, 255, 204, 0.2); background: rgba(0, 255, 204, 0.02); }
        .pulse-success { animation: pulse 2s infinite; }
        @keyframes pulse { 0% { box-shadow: 0 0 0 0 rgba(0, 255, 204, 0.4); } 70% { box-shadow: 0 0 0 10px rgba(0, 255, 204, 0); } 100% { box-shadow: 0 0 0 0 rgba(0, 255, 204, 0); } }
        .sparkline-bar { width: 4px; border-radius: 2px; background: #2a3544; transition: height 0.3s ease; }
        .sparkline-bar.active { background: #00D7FF; box-shadow: 0 0 8px rgba(0, 215, 255, 0.5); }
        ::-webkit-scrollbar { width: 4px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 10px; }
        .material-symbols-rounded { font-variation-settings: 'FILL' 0, 'wght' 300, 'GRAD' 0, 'opsz' 24; }
        .active-icon { font-variation-settings: 'FILL' 1, 'wght' 400; }
    </style>
</head>
<body class="min-h-screen overflow-hidden select-none flex">
    <!-- Sidebar -->
    <aside class="w-16 flex-shrink-0 bg-sidebar border-r border-white/5 flex flex-col items-center py-6 gap-8">
        <div class="flex flex-col items-center gap-1 group cursor-pointer" onclick="openUsernameModal()">
            <div class="w-10 h-10 rounded-xl glass flex items-center justify-center overflow-hidden border-white/10 group-hover:border-accent/50 transition-all">
                <img id="user-avatar" src="" class="w-full h-full object-cover hidden"/>
                <span id="user-initial" class="material-symbols-rounded text-muted">person</span>
            </div>
            <span class="text-[9px] font-bold text-accent tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">PRO</span>
        </div>

        <nav class="flex flex-col gap-6 w-full items-center">
            <button onclick="setView('protection')" class="sidebar-item w-full flex justify-center py-3 text-muted hover:text-white transition-all active" id="nav-protection">
                <span class="material-symbols-rounded">shield</span>
            </button>
            <button onclick="setView('nodes')" class="sidebar-item w-full flex justify-center py-3 text-muted hover:text-white transition-all" id="nav-nodes">
                <span class="material-symbols-rounded">dns</span>
            </button>
            <button onclick="openUsernameModal()" class="sidebar-item w-full flex justify-center py-3 text-muted hover:text-white transition-all">
                <span class="material-symbols-rounded">settings</span>
            </button>
            <button onclick="handleTerminalUI()" class="sidebar-item w-full flex justify-center py-3 text-muted hover:text-white transition-all">
                <span class="material-symbols-rounded">terminal</span>
            </button>
        </nav>

        <div class="mt-auto">
            <button onclick="exitApp()" class="w-10 h-10 flex items-center justify-center text-muted hover:text-red-400 transition-all">
                <span class="material-symbols-rounded">logout</span>
            </button>
        </div>
    </aside>

    <!-- Main Content -->
    <main class="flex-1 flex flex-col min-w-0 bg-surface">
        <!-- Header -->
        <header class="h-20 px-6 flex items-center justify-between border-b border-white/5">
            <div>
                <h1 class="text-xl font-bold tracking-tight text-white flex items-center gap-2">
                    RCP <span class="text-accent">Light</span>
                </h1>
                <p class="text-[10px] text-muted font-mono tracking-widest uppercase mt-0.5">v1.2.0-STABLE</p>
            </div>
            <button onclick="refresh()" class="w-8 h-8 rounded-lg glass flex items-center justify-center hover:text-accent transition-all">
                <span class="material-symbols-rounded text-lg">open_in_new</span>
            </button>
        </header>

        <!-- Scroll Area -->
        <div class="flex-1 overflow-y-auto px-6 py-6" id="content-area">
            <!-- Protection View (Default) -->
            <div id="view-protection" class="flex flex-col gap-6">
                <div class="flex gap-4">
                    <button onclick="openTUI()" class="flex-1 glass rounded-2xl p-4 flex flex-col items-center gap-2 hover:border-primary/50 transition-all group">
                        <span class="material-symbols-rounded text-primary group-hover:scale-110 transition-transform">dashboard</span>
                        <span class="text-[10px] font-bold uppercase tracking-wider">Open TUI</span>
                    </button>
                    <button onclick="handleTerminalUI()" class="flex-1 glass rounded-2xl p-4 flex flex-col items-center gap-2 hover:border-accent/50 transition-all group">
                        <span class="material-symbols-rounded text-accent group-hover:scale-110 transition-transform">terminal</span>
                        <span class="text-[10px] font-bold uppercase tracking-wider">Terminal UI</span>
                    </button>
                </div>

                <div class="flex justify-between items-end">
                    <h2 class="text-xs font-bold text-muted uppercase tracking-widest">Active Nodes</h2>
                    <span id="active-count" class="text-[10px] font-bold text-accent uppercase tracking-tighter">0 NODES ONLINE</span>
                </div>

                <div id="active-profiles" class="flex flex-col gap-3">
                    <!-- Dynamic Profile Cards -->
                </div>
            </div>

            <!-- Nodes View (Full List) -->
            <div id="view-nodes" class="hidden flex flex-col gap-4">
                <div class="flex justify-between items-center">
                    <h2 class="text-xs font-bold text-muted uppercase tracking-widest">All Profiles</h2>
                    <button onclick="handleImportAction()" class="flex items-center gap-1 px-2 py-1 rounded bg-accent/10 text-accent text-[10px] font-bold hover:bg-accent/20 transition-all">
                        <span class="material-symbols-rounded text-sm">add</span> IMPORT
                    </button>
                </div>
                <div id="all-profiles" class="flex flex-col gap-2">
                    <!-- All profiles list -->
                </div>
            </div>
        </div>

        <!-- Footer Stats -->
        <footer class="h-20 px-6 bg-sidebar/50 border-t border-white/5 flex items-center justify-between">
            <div class="flex items-center gap-3">
                <div id="status-dot-footer" class="w-2.5 h-2.5 rounded-full bg-muted shadow-[0_0_8px_rgba(100,116,139,0.3)]"></div>
                <div>
                    <p class="text-[9px] font-bold text-muted uppercase tracking-wider">Protected Session</p>
                    <p id="footer-status-text" class="text-[11px] font-bold text-white uppercase tracking-tight">System Offline</p>
                </div>
            </div>
            <div class="flex items-center gap-6">
                <div class="flex flex-col items-end">
                    <div class="flex items-center gap-1 text-accent">
                        <span class="material-symbols-rounded text-sm">arrow_upward</span>
                        <span id="footer-up" class="font-mono text-[11px] font-bold">0.0MB/s</span>
                    </div>
                    <div class="flex items-center gap-1 text-success">
                        <span class="material-symbols-rounded text-sm">arrow_downward</span>
                        <span id="footer-down" class="font-mono text-[11px] font-bold">0.0MB/s</span>
                    </div>
                </div>
            </div>
        </footer>
    </main>

    <!-- Modals (Simplified versions) -->
    <div id="auth-modal" class="fixed inset-0 z-50 hidden flex items-center justify-center bg-black/60 backdrop-blur-sm p-6">
        <div class="w-full glass rounded-2xl overflow-hidden shadow-2xl border-accent/20">
            <div class="p-6 border-b border-white/5">
                <p id="auth-profile" class="text-[10px] font-bold text-accent uppercase tracking-widest">Profile: ---</p>
                <h3 class="text-lg font-bold text-white mt-1">Connect Node</h3>
            </div>
            <div class="p-6 flex flex-col gap-4">
                <div id="auth-error" class="hidden p-2 bg-red-500/10 border border-red-500/20 rounded text-[10px] text-red-400 font-bold uppercase text-center"></div>
                <div class="flex flex-col gap-1">
                    <label class="text-[9px] text-muted font-bold uppercase tracking-widest px-1">Username</label>
                    <input id="auth-user" type="text" class="w-full bg-white/5 border-b border-white/10 p-2 text-white outline-none focus:border-accent transition-all font-mono text-sm">
                </div>
                <div class="flex flex-col gap-1">
                    <label class="text-[9px] text-muted font-bold uppercase tracking-widest px-1">Token / Password</label>
                    <input id="auth-pass" type="password" class="w-full bg-white/5 border-b border-white/10 p-2 text-white outline-none focus:border-accent transition-all font-mono text-sm tracking-widest">
                </div>
                <div class="flex items-center gap-2 cursor-pointer" onclick="toggleSaveCreds()">
                    <div id="save-check" class="w-4 h-4 rounded border border-white/20 flex items-center justify-center transition-all">
                        <div class="w-2 h-2 bg-accent rounded-sm opacity-0 transition-opacity"></div>
                    </div>
                    <span class="text-[10px] text-muted font-bold uppercase tracking-wider">Save for session</span>
                </div>
                <button id="auth-btn" class="w-full py-3 bg-primary text-white font-bold rounded-xl shadow-lg active:scale-95 transition-all mt-2 text-xs uppercase tracking-widest">Authorize Node</button>
                <button onclick="closeAuthModal()" class="w-full py-2 text-[10px] text-muted hover:text-white uppercase font-bold tracking-widest">Cancel</button>
            </div>
        </div>
    </div>

    <!-- Username/Credentials Modal -->
    <div id="settings-modal" class="fixed inset-0 z-50 hidden flex items-center justify-center bg-black/60 backdrop-blur-sm p-6">
        <div class="w-full glass rounded-2xl overflow-hidden shadow-2xl border-primary/20">
            <div class="p-6 border-b border-white/5">
                <p class="text-[10px] font-bold text-primary uppercase tracking-widest">Settings</p>
                <h3 class="text-lg font-bold text-white mt-1">Global Credentials</h3>
            </div>
            <div class="p-6 flex flex-col gap-4">
                <div class="flex flex-col gap-1">
                    <label class="text-[9px] text-muted font-bold uppercase tracking-widest px-1">Default User</label>
                    <input id="settings-user" type="text" class="w-full bg-white/5 border-b border-white/10 p-2 text-white outline-none focus:border-primary transition-all font-mono text-sm">
                </div>
                <div class="flex flex-col gap-1">
                    <label class="text-[9px] text-muted font-bold uppercase tracking-widest px-1">Global Token</label>
                    <input id="settings-pass" type="password" placeholder="LEAVE BLANK TO SKIP" class="w-full bg-white/5 border-b border-white/10 p-2 text-white outline-none focus:border-primary transition-all font-mono text-sm tracking-widest">
                </div>
                <button onclick="saveSettings()" class="w-full py-3 bg-primary text-white font-bold rounded-xl shadow-lg active:scale-95 transition-all mt-2 text-xs uppercase tracking-widest">Update Settings</button>
                <button onclick="closeSettingsModal()" class="w-full py-2 text-[10px] text-muted hover:text-white uppercase font-bold tracking-widest">Cancel</button>
            </div>
        </div>
    </div>

    <script>
        let currentView = 'protection';
        let selectedProfile = '';
        let saveCreds = false;
        let profileStats = {};
        let globalData = {};
        
        // Activity simulation data
        const activityBuffers = {};

        async function refresh() {
            try {
                const data = await getVpnData();
                if (!data) return;
                globalData = data;
                
                // Update User Info
                document.getElementById('user-initial').innerText = (data.username || 'U')[0].toUpperCase();
                if (data.iconBase64) {
                    const img = document.getElementById('user-avatar');
                    img.src = 'data:image/png;base64,' + data.iconBase64;
                    img.classList.remove('hidden');
                    document.getElementById('user-initial').classList.add('hidden');
                }

                const profiles = data.profiles || [];
                const stats = data.stats || {};
                profileStats = stats;
                
                let activeCount = 0;
                let totalDown = 0;
                let totalUp = 0;
                
                const activeContainer = document.getElementById('active-profiles');
                const allContainer = document.getElementById('all-profiles');
                
                let activeHtml = '';
                let allHtml = '';
                
                profiles.forEach(p => {
                    const s = stats[p] || { connected: false, ip: '', down: '0 B/s', up: '0 B/s' };
                    if (s.connected) {
                        activeCount++;
                        // Parse speeds for totals (rough estimation)
                        const dVal = parseFloat(s.down) || 0;
                        const uVal = parseFloat(s.up) || 0;
                        totalDown += dVal;
                        totalUp += uVal;
                    }
                    
                    const cardClass = s.connected ? 'connected' : '';
                    const statusText = s.connected ? 'CONNECTED' : 'READY';
                    const statusDot = s.connected ? 'bg-success pulse-success' : 'bg-muted/30';
                    const typeText = 'OPENVPN'; // Default for RCP Light
                    
                    const cardHtml = 
                        '<div class="node-card glass rounded-2xl p-4 flex flex-col gap-4 ' + cardClass + '" ondblclick="toggleConnection(\'' + p + '\')">' +
                            '<div class="flex items-center justify-between">' +
                                '<div class="flex items-center gap-3">' +
                                    '<div class="w-10 h-10 rounded-full bg-panel flex items-center justify-center">' +
                                        '<span class="material-symbols-rounded text-accent ' + (s.connected ? 'active-icon' : '') + '">' + (s.connected ? 'public' : 'settings_input_antenna') + '</span>' +
                                    '</div>' +
                                    '<div>' +
                                        '<h4 class="font-bold text-sm text-white">' + p + '</h4>' +
                                        '<div class="flex items-center gap-1.5">' +
                                            '<div class="w-1.5 h-1.5 rounded-full ' + statusDot + '"></div>' +
                                            '<span class="text-[9px] font-bold text-muted uppercase tracking-wider">' + statusText + ' — ' + typeText + '</span>' +
                                        '</div>' +
                                    '</div>' +
                                '</div>' +
                                '<div class="flex flex-col items-end">' +
                                    '<span class="font-mono text-[10px] text-muted">' + (s.connected ? s.ip : '---.---.---.---') + '</span>' +
                                    '<button onclick="toggleConnection(\'' + p + '\')" class="text-accent hover:text-white transition-colors">' +
                                        '<span class="material-symbols-rounded text-lg">' + (s.connected ? 'power_off' : 'play_arrow') + '</span>' +
                                    '</button>' +
                                '</div>' +
                            '</div>' +
                            '<div class="flex items-end justify-between h-8 gap-1 px-1">' +
                                generateSparkline(p, s.connected) +
                                '<span class="font-mono text-[9px] text-accent font-bold">' + (s.connected ? s.down : '0ms') + '</span>' +
                            '</div>' +
                        '</div>';
                    
                    if (s.connected) {
                        activeHtml += cardHtml;
                    }
                    
                    allHtml += 
                        '<div class="glass rounded-xl p-3 flex items-center justify-between hover:bg-white/5 transition-all">' +
                            '<div class="flex items-center gap-3">' +
                                '<span class="material-symbols-rounded text-muted text-sm">' + (s.connected ? 'shield' : 'dns') + '</span>' +
                                '<span class="text-xs font-bold text-white">' + p + '</span>' +
                            '</div>' +
                            '<div class="flex items-center gap-4">' +
                                '<span class="font-mono text-[9px] text-muted">' + (s.connected ? 'ONLINE' : 'OFFLINE') + '</span>' +
                                '<button onclick="event.stopPropagation(); deleteProfile(\'' + p + '\'); refresh();" class="text-muted hover:text-red-400">' +
                                    '<span class="material-symbols-rounded text-sm">delete</span>' +
                                '</button>' +
                            '</div>' +
                        '</div>';
                });
                
                activeContainer.innerHTML = activeHtml || '<div class="p-8 text-center glass rounded-2xl text-[10px] text-muted font-bold uppercase tracking-widest opacity-50">No Active Connections</div>';
                allContainer.innerHTML = allHtml || '<div class="p-8 text-center text-[10px] text-muted uppercase tracking-widest">No Profiles Found</div>';
                
                // Update Global Stats
                document.getElementById('active-count').innerText = activeCount + ' NODES ONLINE';
                const footerDot = document.getElementById('status-dot-footer');
                const footerStatus = document.getElementById('footer-status-text');
                
                if (activeCount > 0) {
                    footerDot.classList.replace('bg-muted', 'bg-success');
                    footerDot.classList.add('pulse-success');
                    footerStatus.innerText = 'System Protected';
                    footerStatus.classList.add('text-success');
                    document.getElementById('footer-up').innerText = totalUp.toFixed(1) + 'MB/s';
                    document.getElementById('footer-down').innerText = totalDown.toFixed(1) + 'MB/s';
                } else {
                    footerDot.classList.replace('bg-success', 'bg-muted');
                    footerDot.classList.remove('pulse-success');
                    footerStatus.innerText = 'System Offline';
                    footerStatus.classList.remove('text-success');
                    document.getElementById('footer-up').innerText = '0.0MB/s';
                    document.getElementById('footer-down').innerText = '0.0MB/s';
                }
                
            } catch (e) { console.error(e); }
        }

        function generateSparkline(p, active) {
            if (!activityBuffers[p]) activityBuffers[p] = Array(12).fill(0).map(() => Math.random() * 10);
            
            // Shift buffer and add new random point if active
            activityBuffers[p].shift();
            activityBuffers[p].push(active ? Math.random() * 25 + 5 : Math.random() * 3);
            
            return activityBuffers[p].map((v, i) => {
                const height = Math.max(4, v);
                const isActive = active && i > 8;
                return '<div class="sparkline-bar ' + (isActive ? 'active' : '') + '" style="height: ' + height + 'px"></div>';
            }).join('');
        }

        function setView(view) {
            currentView = view;
            document.querySelectorAll('.sidebar-item').forEach(el => el.classList.remove('active'));
            document.getElementById('nav-' + view).classList.add('active');
            
            document.getElementById('view-protection').classList.add('hidden');
            document.getElementById('view-nodes').classList.add('hidden');
            document.getElementById('view-' + view).classList.remove('hidden');
        }

        function toggleConnection(p) {
            const s = profileStats[p] || { connected: false };
            if (s.connected) {
                disconnect(p);
                refresh();
            } else {
                openAuthModal(p);
            }
        }

        function openAuthModal(p) {
            selectedProfile = p;
            document.getElementById('auth-profile').innerText = 'Profile: ' + p;
            document.getElementById('auth-user').value = globalData.sharedUser || globalData.username || '';
            document.getElementById('auth-pass').value = globalData.sharedPass || '';
            saveCreds = !!globalData.sharedPass;
            updateCheck();
            document.getElementById('auth-modal').classList.remove('hidden');
            document.getElementById('auth-user').focus();
        }

        function closeAuthModal() { document.getElementById('auth-modal').classList.add('hidden'); }

        function toggleSaveCreds() { saveCreds = !saveCreds; updateCheck(); }
        function updateCheck() {
            const check = document.getElementById('save-check');
            if (saveCreds) {
                check.classList.add('border-accent', 'bg-accent/10');
                check.querySelector('div').classList.remove('opacity-0');
            } else {
                check.classList.remove('border-accent', 'bg-accent/10');
                check.querySelector('div').classList.add('opacity-0');
            }
        }

        document.getElementById('auth-btn').onclick = async () => {
            const user = document.getElementById('auth-user').value.trim();
            const pass = document.getElementById('auth-pass').value;
            if (!user || !pass) return;
            
            const btn = document.getElementById('auth-btn');
            const errDiv = document.getElementById('auth-error');
            btn.innerText = 'CONNECTING...';
            btn.disabled = true;
            
            if (saveCreds) {
                await saveCredentials(user, pass);
            }
            
            const err = await connect(selectedProfile, user, pass);
            if (err) {
                errDiv.innerText = err;
                errDiv.classList.remove('hidden');
                btn.innerText = 'AUTHORIZE NODE';
                btn.disabled = false;
            } else {
                closeAuthModal();
                refresh();
            }
        };

        function openUsernameModal() {
            document.getElementById('settings-user').value = globalData.username;
            document.getElementById('settings-pass').value = globalData.sharedPass || '';
            document.getElementById('settings-modal').classList.remove('hidden');
        }

        function closeSettingsModal() { document.getElementById('settings-modal').classList.add('hidden'); }

        async function saveSettings() {
            const user = document.getElementById('settings-user').value.trim();
            const pass = document.getElementById('settings-pass').value;
            if (user) {
                await saveCredentials(user, pass);
                closeSettingsModal();
                refresh();
            }
        }

        function handleTerminalUI() { openTUI(); }
        
        async function handleImportAction() {
            const res = await importProfile();
            if (res && res.path && res.name) {
                await saveImport(res.path, res.name);
                refresh();
                setView('nodes');
            }
        }

        // Global key handlers
        window.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                closeAuthModal();
                closeSettingsModal();
            }
        });

        setInterval(refresh, 1000);
        refresh();
    </script>
</body>
</html>
	`
}
