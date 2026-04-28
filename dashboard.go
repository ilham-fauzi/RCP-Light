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
    [window setStyleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable];
    [window setTitleVisibility:NSWindowTitleHidden];
    [window setTitlebarAppearsTransparent:YES];
    [window setStyleMask:[window styleMask] | NSWindowStyleMaskFullSizeContentView];
    [window setHasShadow:YES];
    [window setBackgroundColor:[NSColor colorWithRed:0.06 green:0.08 blue:0.10 alpha:1.0]];
    [window setMovableByWindowBackground:YES];
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
	d.w.SetSize(900, 650, webview.HintFixed)

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
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;700&family=Space+Grotesk:wght@500;700&family=Material+Symbols+Outlined:wght,FILL@100..700,0..1&display=swap" rel="stylesheet"/>
    <script>
        tailwind.config = {
            darkMode: "class",
            theme: {
                extend: {
                    "colors": {
                        "surface": "#0f1419",
                        "on-surface": "#dfe3ea",
                        "primary": "#5F5CF1",
                        "secondary": "#00FF88",
                        "outline": "#88919d",
                    },
                    "fontFamily": {
                        "label-caps": ["Space Grotesk"],
                        "status-display": ["Space Grotesk"],
                    }
                }
            }
        }
    </script>
    <style>
        .glass-panel { background: rgba(15, 20, 25, 0.4); backdrop-filter: blur(12px); border: 1px solid rgba(255, 255, 255, 0.1); }
        .active-glow { box-shadow: 0 0 20px rgba(0, 163, 255, 0.15), inset 0 0 10px rgba(0, 163, 255, 0.05); }
        .pulse-dot { animation: pulse 2s infinite; }
        @keyframes pulse { 0%, 100% { opacity: 0.4; transform: scale(0.8); } 50% { opacity: 1; transform: scale(1); } }
        .material-symbols-outlined { font-variation-settings: 'FILL' 0, 'wght' 200, 'GRAD' 0, 'opsz' 24; }
        .power-btn-active:hover #status-ring { border-color: rgba(239, 68, 68, 0.4) !important; }
        .power-btn-active:hover #power-icon-bg { background: rgba(239, 68, 68, 0.1) !important; }
        .power-btn-active:hover #power-icon { color: #ef4444 !important; }
        .power-btn-inactive:hover #status-ring { border-color: rgba(0, 255, 136, 0.4) !important; }
        .power-btn-inactive:hover #power-icon-bg { background: rgba(0, 255, 136, 0.1) !important; }
        .power-btn-inactive:hover #power-icon { color: #00FF88 !important; }
        #apply-to-all:checked + div .checked-icon { opacity: 1; }
        #apply-to-all:checked + div { border-color: #5F5CF1; background: rgba(95, 92, 241, 0.1); }
        button:disabled { opacity: 0.5; cursor: not-allowed; filter: grayscale(1); }
    </style>
</head>
<body class="bg-surface text-on-surface font-sans min-h-screen overflow-hidden select-none">
    <div id="app">
        <header class="fixed top-0 w-full z-50 flex justify-between items-center px-6 h-16 bg-slate-950/60 backdrop-blur-md border-b border-white/10">
            <div class="flex items-center gap-3">
                <img id="app-icon" src="" class="w-8 h-8 drop-shadow-[0_0_10px_rgba(95,92,241,0.5)] hidden" />
                <span class="text-white font-black tracking-tighter drop-shadow-[0_0_10px_rgba(95,92,241,0.5)] uppercase">RCP LIGHT V1.2.0</span>
            </div>
            <div class="flex gap-4 items-center">
                <div onclick="openUsernameModal()" class="flex items-center gap-2 px-3 py-1.5 bg-white/5 rounded-full border border-white/10 hover:border-primary/50 cursor-pointer transition-all group">
                    <span class="material-symbols-outlined text-[12px] text-outline group-hover:text-primary transition-colors">person</span>
                    <span id="header-username" class="text-[10px] font-mono text-outline uppercase tracking-wider group-hover:text-on-surface leading-none">---</span>
                </div>
                <span onclick="refresh()" class="material-symbols-outlined text-slate-500 cursor-pointer hover:text-primary transition-colors">refresh</span>
            </div>
        </header>

        <main class="relative z-10 pt-24 pb-32 px-6 max-w-6xl mx-auto grid grid-cols-1 md:grid-cols-12 gap-6">
            <div class="md:col-span-8 flex flex-col gap-4">
                <h2 class="text-on-surface font-bold tracking-tight uppercase opacity-50 text-xs">VPN PROFILES</h2>
                <div id="profile-list" class="flex flex-col gap-3">
                    <div class="p-8 text-center text-outline animate-pulse">LOADING PROFILES...</div>
                </div>
            </div>

            <div class="md:col-span-4 flex flex-col gap-6">
                <div id="power-btn-container" onclick="handlePowerBtn()" class="glass-panel rounded-xl p-6 flex flex-col items-center justify-center gap-6 cursor-pointer transition-all border border-transparent">
                    <div class="relative w-24 h-24 flex items-center justify-center">
                        <div id="status-ring" class="absolute inset-0 border-2 border-white/5 rounded-full transition-all duration-500"></div>
                        <div id="power-icon-bg" class="w-16 h-16 rounded-full bg-white/5 flex items-center justify-center transition-all duration-500">
                            <span id="power-icon" class="material-symbols-outlined text-outline text-3xl transition-all duration-500">power_settings_new</span>
                        </div>
                    </div>
                    <div class="text-center">
                        <p class="text-[10px] font-label-caps text-outline uppercase tracking-widest opacity-50">System Status</p>
                        <p id="system-status" class="font-status-display text-outline transition-all duration-500 uppercase">OFFLINE</p>
                    </div>
                </div>

                <div class="glass-panel rounded-xl p-6 flex flex-col gap-4">
                    <div class="flex justify-between items-center">
                        <span class="text-secondary material-symbols-outlined">arrow_downward</span>
                        <div class="text-right">
                            <p class="text-[9px] font-label-caps text-outline uppercase">Download</p>
                            <p id="stat-down" class="font-mono text-sm">0 B/s</p>
                        </div>
                    </div>
                    <div class="flex justify-between items-center">
                        <span class="text-primary material-symbols-outlined">arrow_upward</span>
                        <div class="text-right">
                            <p class="text-[9px] font-label-caps text-outline uppercase">Upload</p>
                            <p id="stat-up" class="font-mono text-sm">0 B/s</p>
                        </div>
                    </div>
                </div>
            </div>
        </main>

        <nav class="fixed bottom-0 left-0 w-full z-50 flex justify-around items-center px-4 py-3 bg-slate-950/40 backdrop-blur-xl border-t border-white/10">
            <div class="absolute -top-6 left-0 w-full text-center">
                <span id="path-info" class="text-[8px] text-outline/50 font-mono uppercase tracking-widest"></span>
            </div>
            <button onclick="handleImportAction()" class="flex flex-col items-center text-slate-400 hover:text-primary transition-all">
                <span class="material-symbols-outlined">input</span>
                <span class="font-mono text-[10px]">IMPORT</span>
            </button>
            <button onclick="exitApp()" class="flex flex-col items-center text-slate-400 hover:text-red-500 transition-all">
                <span class="material-symbols-outlined text-red-500/50 group-hover:text-red-500">logout</span>
                <span class="font-mono text-[10px]">EXIT</span>
            </button>
        </nav>

        <div id="password-modal" class="fixed inset-0 z-[100] hidden flex items-center justify-center bg-surface/60 backdrop-blur-sm">
            <div class="w-full max-w-md mx-4 glass-panel rounded-xl overflow-hidden shadow-2xl">
                <div class="p-6 border-b border-white/5">
                    <span id="modal-profile-name" class="text-[10px] font-label-caps text-outline uppercase tracking-widest">Profile: ---</span>
                    <h1 class="text-xl font-bold text-on-surface mt-1">AUTHORIZE CONNECTION</h1>
                </div>
                <div id="auth-error" class="hidden mx-6 mt-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg">
                    <div class="flex items-center gap-2 text-red-500">
                        <span class="material-symbols-outlined text-sm">error</span>
                        <span id="auth-error-msg" class="text-[10px] font-bold uppercase tracking-wider">Authentication Failed</span>
                    </div>
                </div>
                <div class="p-6 flex flex-col gap-4">
                    <div class="flex flex-col gap-1">
                        <span class="text-[9px] text-outline uppercase tracking-widest px-1">Username</span>
                        <input id="vpn-username" type="text" placeholder="USERNAME" class="w-full bg-white/5 border-b border-white/20 p-2 text-on-surface outline-none focus:border-primary transition-all">
                    </div>

                    <div class="flex flex-col gap-1">
                        <span class="text-[9px] text-outline uppercase tracking-widest px-1">Token / Password</span>
                        <input id="vpn-password" type="password" placeholder="ENTER TOKEN" class="w-full bg-white/5 border-b border-white/20 p-2 text-xl tracking-[0.3em] outline-none focus:border-primary transition-all">
                    </div>
                    
                    <label class="flex items-center gap-2 cursor-pointer group mt-1">
                        <input id="apply-to-all" type="checkbox" class="hidden">
                        <div class="w-4 h-4 rounded border border-white/20 group-hover:border-primary transition-all flex items-center justify-center">
                            <div class="w-2 h-2 bg-primary rounded-sm opacity-0 transition-all checked-icon"></div>
                        </div>
                        <span class="text-[10px] text-outline group-hover:text-on-surface transition-all uppercase tracking-wider">Apply for this session</span>
                    </label>

                    <button id="auth-btn" class="w-full py-4 bg-primary text-white font-bold rounded-lg uppercase tracking-widest shadow-lg active:scale-95 transition-all mt-2">Authorize Connection</button>
                    <div class="h-2"></div>
                    <button onclick="closeModal()" class="w-full text-[10px] text-outline uppercase hover:text-white transition-colors py-2">Cancel</button>
                </div>
            </div>
        </div>

        <div id="rename-modal" class="fixed inset-0 z-[110] hidden flex items-center justify-center bg-surface/80 backdrop-blur-md">
            <div class="w-full max-w-md mx-4 glass-panel rounded-xl overflow-hidden shadow-2xl border border-primary/20">
                <div class="p-6 border-b border-white/5">
                    <span class="text-[10px] font-label-caps text-outline uppercase tracking-widest">Naming Required</span>
                    <h1 class="text-xl font-bold text-on-surface mt-1">RENAME PROFILE</h1>
                </div>
                <div class="p-6 flex flex-col gap-4">
                    <input id="new-profile-name" type="text" maxlength="10" class="w-full bg-white/5 border-b border-primary/30 p-2 text-xl font-bold outline-none focus:border-primary transition-all uppercase tracking-wider text-center">
                    <button id="save-import-btn" class="w-full py-4 bg-primary text-white font-bold rounded-lg uppercase tracking-widest active:scale-95 transition-all">Save Profile</button>
                    <button onclick="closeRenameModal()" class="w-full text-[10px] text-outline uppercase">Cancel</button>
                </div>
            </div>
        </div>

        <div id="username-modal" class="fixed inset-0 z-[120] hidden flex items-center justify-center bg-surface/80 backdrop-blur-md">
            <div class="w-full max-w-sm mx-4 glass-panel rounded-xl overflow-hidden shadow-2xl border border-primary/20">
                <div class="p-6 border-b border-white/5">
                    <span class="text-[10px] font-label-caps text-outline uppercase tracking-widest">Global Credentials</span>
                    <h1 class="text-xl font-bold text-on-surface mt-1">EDIT CREDENTIALS</h1>
                </div>
                <div class="p-6 flex flex-col gap-4">
                    <div class="flex flex-col gap-1">
                        <span class="text-[9px] text-outline uppercase tracking-widest px-1">Username</span>
                        <input id="new-username" type="text" class="w-full bg-white/5 border-b border-primary/30 p-2 text-lg font-bold outline-none focus:border-primary transition-all text-center">
                    </div>
                    <div class="flex flex-col gap-1">
                        <span class="text-[9px] text-outline uppercase tracking-widest px-1">Token / Password</span>
                        <input id="new-password" type="password" placeholder="LEAVE EMPTY TO CLEAR" class="w-full bg-white/5 border-b border-primary/30 p-2 text-lg tracking-[0.3em] outline-none focus:border-primary transition-all text-center">
                    </div>
                    <button id="save-username-btn" class="w-full py-4 bg-primary text-white font-bold rounded-lg uppercase tracking-widest active:scale-95 transition-all mt-2">Update Credentials</button>
                    <button onclick="closeUsernameModal()" class="w-full text-[10px] text-outline uppercase">Cancel</button>
                </div>
            </div>
        </div>
    </div>

    <script>
        let selectedProfile = '';
        let currentStats = {};
        let pendingImport = null;
        let sharedPassword = '';
        let sharedUsername = '';
        let globalUsername = '';

        async function refresh() {
            try {
                const data = await getVpnData();
                if (!data) return;
 
                 document.getElementById('path-info').innerText = 'CONFIG_PATH: ' + data.configDir;
                 globalUsername = data.username || 'vpnuser';
                 document.getElementById('header-username').innerText = globalUsername;
                 sharedPassword = data.sharedPass || '';
                 sharedUsername = data.sharedUser || globalUsername;
                 
                 if (data.iconBase64) {
                     const img = document.getElementById('app-icon');
                     img.src = 'data:image/png;base64,' + data.iconBase64;
                     img.classList.remove('hidden');
                 }
                 
                 const profiles = data.profiles || [];
                currentStats = data.stats || {};
                const list = document.getElementById('profile-list');
                
                let html = '';
                const powerBtn = document.getElementById('power-btn-container');
                const powerIcon = document.getElementById('power-icon');
                const powerIconBg = document.getElementById('power-icon-bg');
                const statusRing = document.getElementById('status-ring');
                const systemStatus = document.getElementById('system-status');

                // 1. GLOBAL SYSTEM STATUS
                let globalConnected = false;
                profiles.forEach(p => { if ((currentStats[p] || {}).connected) globalConnected = true; });

                if (globalConnected) {
                    systemStatus.innerText = 'PROTECTED';
                    systemStatus.classList.add('text-secondary');
                    systemStatus.classList.remove('text-outline');
                } else {
                    systemStatus.innerText = 'OFFLINE';
                    systemStatus.classList.remove('text-secondary');
                    systemStatus.classList.add('text-outline');
                    document.getElementById('stat-down').innerText = '0 B/s';
                    document.getElementById('stat-up').innerText = '0 B/s';
                }

                // 2. PROFILE LIST & SELECTION STATUS
                profiles.forEach(p => {
                    const s = currentStats[p] || { connected: false, ip: '', down: '0 B/s', up: '0 B/s' };
                    const isSelected = selectedProfile === p;
                    
                    if (s.connected) {
                        document.getElementById('stat-down').innerText = s.down;
                        document.getElementById('stat-up').innerText = s.up;
                    }

                    // Power Button Visuals (Based on SELECTED profile)
                    if (isSelected) {
                        if (s.connected) {
                            statusRing.classList.add('border-secondary');
                            statusRing.classList.remove('border-white/5');
                            powerIcon.classList.add('text-secondary');
                            powerIcon.classList.remove('text-outline');
                            powerIconBg.classList.add('bg-secondary/10', 'active-glow');
                            powerIconBg.classList.remove('bg-white/5');
                            powerBtn.classList.add('power-btn-active');
                            powerBtn.classList.remove('power-btn-inactive');
                        } else {
                            statusRing.classList.remove('border-secondary');
                            statusRing.classList.add('border-white/5');
                            powerIcon.classList.remove('text-secondary');
                            powerIcon.classList.add('text-outline');
                            powerIconBg.classList.remove('bg-secondary/10', 'active-glow');
                            powerIconBg.classList.add('bg-white/5');
                            powerBtn.classList.remove('power-btn-active');
                            powerBtn.classList.add('power-btn-inactive');
                        }
                    }

                    const activeClass = s.connected ? 'active-glow border-secondary/30 bg-secondary/5' : (isSelected ? 'bg-primary/20 border-primary/40' : 'hover:bg-white/5');
                    const statusDot = s.connected ? '<span class="w-2 h-2 rounded-full bg-secondary pulse-dot"></span>' : '<span class="w-2 h-2 rounded-full bg-outline/20"></span>';
                    
                    html += '<div onclick="selectProfile(\'' + p + '\')" ondblclick="handleAction(\'' + p + '\', ' + s.connected + ')" class="glass-panel rounded-xl p-4 flex items-center justify-between transition-all cursor-pointer border ' + activeClass + '">' +
                            '<div class="flex items-center gap-4">' +
                                '<span class="material-symbols-outlined ' + (s.connected ? 'text-secondary' : (isSelected ? 'text-primary' : 'text-outline')) + '">' + (s.connected ? 'shield' : 'dns') + '</span>' +
                                '<div>' +
                                    '<span class="font-bold ' + (s.connected ? 'text-secondary' : 'text-on-surface') + '">' + p + '</span>' +
                                    '<div class="flex items-center gap-2">' + statusDot + '<span class="text-[10px] uppercase text-outline">' + (s.connected ? 'Connected' : 'Disconnected') + '</span></div>' +
                                '</div>' +
                            '</div>' +
                            '<div class="flex items-center gap-6">' +
                                '<span class="font-mono text-xs text-outline">' + (s.connected ? s.ip : '---.---.---.---') + '</span>' +
                                '<span onclick="event.stopPropagation(); deleteProfile(\'' + p + '\'); refresh();" class="material-symbols-outlined text-outline hover:text-red-400 text-sm">delete</span>' +
                            '</div>' +
                        '</div>';
                });

                if (!selectedProfile) {
                    statusRing.classList.remove('border-secondary');
                    statusRing.classList.add('border-white/5');
                    powerIcon.classList.remove('text-secondary');
                    powerIcon.classList.add('text-outline');
                    powerIconBg.classList.remove('bg-secondary/10', 'active-glow');
                    powerIconBg.classList.add('bg-white/5');
                    powerBtn.classList.remove('power-btn-active', 'power-btn-inactive');
                }

                list.innerHTML = html || '<div class="glass-panel rounded-xl p-8 text-center opacity-30 uppercase text-[10px]">No Profiles Found</div>';
            } catch (e) { console.error(e); }
        }

        function selectProfile(p) { selectedProfile = p; refresh(); }

        function handleAction(p, connected) {
            if (connected) { disconnect(p); refresh(); } 
            else { 
                openPasswordModal(p); 
            }
        }

        async function performConnect(p, user, pwd) {
            const btn = document.getElementById('auth-btn');
            const errDiv = document.getElementById('auth-error');
            const errMsg = document.getElementById('auth-error-msg');
            const originalText = btn.innerText;
            
            btn.innerText = 'AUTHORIZING...';
            btn.disabled = true;
            errDiv.classList.add('hidden');
            
            const err = await connect(p, user, pwd);
            if (err) { 
                errMsg.innerText = err;
                errDiv.classList.remove('hidden');
                sharedPassword = ''; 
                sharedUsername = '';
                // Don't call openPasswordModal here as it's already open and would reset the error state
            } else { 
                closeModal(); 
            }
            btn.innerText = originalText;
            btn.disabled = false;
            refresh();
        }

        function handlePowerBtn() {
            if (!selectedProfile) return;
            handleAction(selectedProfile, (currentStats[selectedProfile] || {}).connected);
        }

        function openPasswordModal(p) {
            selectedProfile = p;
            document.getElementById('modal-profile-name').innerText = 'Profile: ' + p;
            
            const userField = document.getElementById('vpn-username');
            userField.value = sharedUsername || globalUsername;
            
            const passField = document.getElementById('vpn-password');
            passField.value = sharedPassword || ''; // Pre-fill synced token visually
            
            document.getElementById('apply-to-all').checked = !!sharedPassword;
            document.getElementById('password-modal').classList.remove('hidden');
            
            if (document.getElementById('auth-btn').innerText !== 'AUTHORIZING...') {
                document.getElementById('auth-error').classList.add('hidden');
            }
            
            // Focus and select username
            userField.focus();
            userField.select();
        }

        function closeModal() { document.getElementById('password-modal').classList.add('hidden'); }

        document.getElementById('auth-btn').onclick = async () => {
            const user = document.getElementById('vpn-username').value.trim();
            let pwd = document.getElementById('vpn-password').value;
            
            // If user left password empty, use the shared one from background
            if (!pwd && sharedPassword) {
                pwd = sharedPassword;
            }
            
            if (!user || !pwd) return;
            
            if (document.getElementById('apply-to-all').checked) {
                sharedPassword = pwd;
                sharedUsername = user;
            } else {
                sharedPassword = '';
                sharedUsername = '';
            }
            
            await performConnect(selectedProfile, user, pwd);
        };

        async function handleImportAction() {
            const res = await importProfile();
            if (res && res.path && res.name) {
                if (res.name.length > 10) {
                    pendingImport = res;
                    document.getElementById('new-profile-name').value = res.name.substring(0, 10).toUpperCase();
                    document.getElementById('rename-modal').classList.remove('hidden');
                } else { await saveImport(res.path, res.name); refresh(); }
            }
        }

        function closeRenameModal() { document.getElementById('rename-modal').classList.add('hidden'); }
 
        document.getElementById('save-import-btn').onclick = async () => {
            const name = document.getElementById('new-profile-name').value.trim();
            if (name && pendingImport) { await saveImport(pendingImport.path, name); closeRenameModal(); refresh(); }
        };

        function openUsernameModal() {
            document.getElementById('new-username').value = document.getElementById('header-username').innerText;
            document.getElementById('new-password').value = sharedPassword;
            document.getElementById('username-modal').classList.remove('hidden');
            document.getElementById('new-username').focus();
        }

        function closeUsernameModal() { document.getElementById('username-modal').classList.add('hidden'); }

        document.getElementById('save-username-btn').onclick = async () => {
            const name = document.getElementById('new-username').value.trim();
            const pass = document.getElementById('new-password').value;
            if (name) {
                await saveCredentials(name, pass);
                closeUsernameModal();
                refresh();
            }
        };

        document.getElementById('vpn-password').onkeydown = (e) => {
            if (e.key === 'Enter') document.getElementById('auth-btn').click();
            if (e.key === 'Escape') closeModal();
        };

        document.getElementById('new-profile-name').onkeydown = (e) => {
            if (e.key === 'Enter') document.getElementById('save-import-btn').click();
            if (e.key === 'Escape') closeRenameModal();
        };

        document.getElementById('new-username').onkeydown = (e) => {
            if (e.key === 'Enter') document.getElementById('save-username-btn').click();
            if (e.key === 'Escape') closeUsernameModal();
        };

        document.getElementById('new-password').onkeydown = (e) => {
            if (e.key === 'Enter') document.getElementById('save-username-btn').click();
            if (e.key === 'Escape') closeUsernameModal();
        };

        window.addEventListener('keydown', (e) => {
            if (e.metaKey) {
                const k = e.key.toLowerCase();
                if (k === 'v') document.execCommand('paste');
                if (k === 'c') document.execCommand('copy');
                if (k === 'x') document.execCommand('cut');
                if (k === 'a') {
                    if (document.activeElement && (document.activeElement.tagName === 'INPUT')) {
                        document.activeElement.select();
                        e.preventDefault();
                    }
                }
            }
            if (e.key === 'Escape') { closeModal(); closeRenameModal(); closeUsernameModal(); }
        });

        setInterval(refresh, 1000);
        refresh();
    </script>
</body>
</html>
	`
}
