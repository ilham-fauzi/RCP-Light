package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

// Called before SetHtml — just sets window chrome/frame
void login_frame(void *w) {
    NSWindow *window = (NSWindow *)w;
    [window setStyleMask:NSWindowStyleMaskTitled
                        | NSWindowStyleMaskClosable
                        | NSWindowStyleMaskFullSizeContentView];
    [window setTitleVisibility:NSWindowTitleHidden];
    [window setTitlebarAppearsTransparent:YES];
    [window setMovableByWindowBackground:YES];
    [window setHasShadow:YES];
    [window setLevel:NSFloatingWindowLevel]; // Keep above other windows
    
    // Position near tray icon (using mouse location as proxy)
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

// Enables standard macOS Edit menu (Copy/Paste/Select All)
void login_menu() {
    NSMenu *mainMenu = [NSApp mainMenu];
    if (mainMenu == nil) {
        mainMenu = [[NSMenu alloc] init];
        [NSApp setMainMenu:mainMenu];
    }
    
    // Check if Edit menu already exists
    for (NSMenuItem *item in [mainMenu itemArray]) {
        if ([[item title] isEqualToString:@"Edit"]) return;
    }
    
    NSMenuItem *editMenuItem = [[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    
    [editMenu addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
    [editMenu addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
    [editMenu addItem:[NSMenuItem separatorItem]];
    [editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
    [editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
    [editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    
    [editMenuItem setSubmenu:editMenu];
    [mainMenu addItem:editMenuItem];
}

// Called AFTER SetHtml — adds frosted glass behind webview without moving it
void login_glass(void *w) {
    NSWindow *window = (NSWindow *)w;
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor clearColor]];

    NSView *contentView = [window contentView];

    // Find the WKWebView and make it transparent
    for (NSView *v in [contentView subviews]) {
        if ([v isKindOfClass:NSClassFromString(@"WKWebView")]) {
            @try {
                // Modern way to make WKWebView transparent
                [(id)v setValue:@NO forKey:@"drawsBackground"];
                if ([(id)v respondsToSelector:@selector(setBackgroundColor:)]) {
                    [(id)v setBackgroundColor:[NSColor clearColor]];
                }
            } @catch (NSException *e) {}
        }
    }

    // Insert NSVisualEffectView BEHIND the webview
    NSVisualEffectView *fx = [[NSVisualEffectView alloc] initWithFrame:[contentView bounds]];
    // Material HUDWindow or Popover for better tray look
    [fx setMaterial:NSVisualEffectMaterialHUDWindow];
    [fx setBlendingMode:NSVisualEffectBlendingModeBehindWindow];
    [fx setState:NSVisualEffectStateActive];
    [fx setAutoresizingMask:NSViewWidthSizable | NSViewHeightSizable];

    // Force dark appearance
    [fx setAppearance:[NSAppearance appearanceNamed:NSAppearanceNameDarkAqua]];
    [window setAppearance:[NSAppearance appearanceNamed:NSAppearanceNameDarkAqua]];

    // Add it at the bottom
    [contentView addSubview:fx positioned:NSWindowBelow relativeTo:nil];
}

void login_focus(void *w) {
    NSWindow *window = (NSWindow *)w;
    [NSApp activateIgnoringOtherApps:YES];
    [window makeKeyAndOrderFront:nil];
    [window orderFrontRegardless];
}
*/
import "C"
import (
	"strings"
	"sync"

	"github.com/webview/webview_go"
)

type loginResult struct {
	user     string
	password string
	applyAll bool
	canceled bool
}

func showLoginWindow(profile string, defaultUser string, defaultPass string) loginResult {
	var result loginResult
	var wg sync.WaitGroup
	wg.Add(1)

	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle("RCP Light")
	w.SetSize(360, 350, webview.HintFixed)
	C.login_frame(w.Window()) // frame only — before content
	C.login_menu()            // Enable copy/paste

	w.Bind("_onSubmit", func(user string, password string, applyAll bool) {
		result = loginResult{user: user, password: password, applyAll: applyAll}
		w.Terminate()
		wg.Done()
	})
	w.Bind("_onCancel", func() {
		result = loginResult{canceled: true}
		w.Terminate()
		wg.Done()
	})

	w.SetHtml(loginHTML(profile, defaultUser, defaultPass))
	C.login_glass(w.Window()) // glass AFTER SetHtml so WKWebView subview exists
	C.login_focus(w.Window())
	w.Run()
	return result
}

func loginHTML(profile, defaultUser string, defaultPass string) string {
	html := strings.ReplaceAll(loginTemplate, "{{PROFILE}}", profile)
	html = strings.ReplaceAll(html, "{{DEFAULT_USER}}", defaultUser)
	html = strings.ReplaceAll(html, "{{DEFAULT_PASS}}", defaultPass)
	if defaultPass != "" {
		html = strings.ReplaceAll(html, "id=\"a\">", "id=\"a\" checked>")
	}
	return html
}

const loginTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
html,body{
  height:100%;
  background:transparent;
  color:#fff;
  font-family:-apple-system,'SF Pro Text','Inter',sans-serif;
  -webkit-font-smoothing:antialiased;
  user-select:none;
  overflow: hidden;
}
body{
  display:flex;
  flex-direction:column;
  justify-content:flex-start;
  padding:38px 28px 20px; /* Slightly reduced top/bottom padding */
  gap:14px; /* Reduced gap */
  -webkit-app-region:drag;
  /* Dark fallback if transparency fails */
  background: rgba(25, 25, 27, 0.85);
}

.chip{
  -webkit-app-region:no-drag;
  align-self:center;
  margin-bottom: 2px;
  font-size:9px;font-weight:700;letter-spacing:.18em;text-transform:uppercase;
  color:rgba(255,255,255,.5);
  background:rgba(255,255,255,.1);
  border:1px solid rgba(255,255,255,.15);
  border-radius:999px;
  padding:3px 12px;
}

.fields{display:flex;flex-direction:column;gap:10px;-webkit-app-region:no-drag}
.field{display:flex;flex-direction:column;gap:4px}
.label{font-size:8.5px;font-weight:600;letter-spacing:.12em;text-transform:uppercase;color:rgba(255,255,255,.45)}
.row{
  display:flex;align-items:center;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 7px 12px;
  transition: all .2s ease;
}
.row:focus-within{
  border-color:#5F5CF1;
  background: rgba(255, 255, 255, 0.09);
  box-shadow: 0 0 0 2px rgba(95, 92, 241, 0.2);
}
.ico{font-size:14px;margin-right:10px;opacity:.6;flex-shrink:0}
input{
  flex:1;background:transparent;border:none;outline:none;
  color:#fff;font-size:14px;font-family:inherit;
  -webkit-user-select:text;user-select:text;
}
input::placeholder{color:rgba(255,255,255,.3)}
.eye{
  background:none;border:none;cursor:pointer;
  font-size:14px;color:rgba(255,255,255,.4);padding:2px;
  transition:color .2s;
}
.eye:hover{color:rgba(255,255,255,.8)}

.err{display:none;font-size:11px;color:#ff6b6b;text-align:center;font-weight:500}
.err.on{display:block}

.actions{display:flex;flex-direction:column;gap:6px;-webkit-app-region:no-drag;margin-top: 4px;}
.btn-main{
  width:100%;padding:11px;
  background:#5F5CF1;border:none;border-radius:8px;
  color:#fff;font-size:12px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;
  cursor:pointer;transition:all .2s ease;
  box-shadow:0 4px 15px rgba(95,92,241,0.4);
}
.btn-main:hover:not(:disabled){background:#6e6cf2;transform:translateY(-1px);box-shadow:0 6px 20px rgba(95,92,241,0.5)}
.btn-main:active:not(:disabled){transform:scale(.98)}
.btn-main:disabled{opacity:.45;cursor:not-allowed}
.btn-cancel{
  background:none;border:none;
  color:rgba(255,255,255,.4);font-size:10.5px;
  cursor:pointer;padding:8px;
  transition:color .18s;font-family:inherit;letter-spacing:.02em;
}
.btn-cancel:hover{color:rgba(255,255,255,.7)}
input[readonly]{opacity:0.6;cursor:default}

.check-row{display:flex;align-items:center;gap:10px;cursor:pointer;-webkit-app-region:no-drag;margin:2px 0 4px;user-select:none}
.check-row input{display:none}
.check-box{
  width:16px;height:16px;border-radius:4px;
  border:1px solid rgba(255,255,255,.15);
  background:rgba(255,255,255,.05);
  display:flex;align-items:center;justify-content:center;
  transition:all .2s;
  flex-shrink:0;
}
.check-row:hover .check-box{border-color:rgba(255,255,255,.4);background:rgba(255,255,255,.08)}
.check-row input:checked + .check-box{background:#5F5CF1;border-color:#5F5CF1;box-shadow:0 0 10px rgba(95,92,241,0.3)}
.check-box::after{
  content:'';width:5px;height:9px;
  border:solid #fff;border-width:0 2px 2px 0;
  transform:rotate(45deg) translate(-1px, -1px);
  display:none;
}
.check-row input:checked + .check-box::after{display:block}
.check-label{font-size:10px;color:rgba(255,255,255,.4);font-weight:600;letter-spacing:.03em;text-transform:uppercase}
.check-row:hover .check-label{color:rgba(255,255,255,.7)}
.lock-ico{font-size:10px;margin-left:auto;opacity:.4}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(12px); }
  to { opacity: 1; transform: translateY(0); }
}
.chip { animation: fadeIn 0.5s cubic-bezier(0.16, 1, 0.3, 1) both; }
.fields { animation: fadeIn 0.5s cubic-bezier(0.16, 1, 0.3, 1) 0.1s both; }
.actions { animation: fadeIn 0.5s cubic-bezier(0.16, 1, 0.3, 1) 0.2s both; }
</style>
</head>
<body>
  <div class="chip">{{PROFILE}}</div>

  <div class="fields">
    <div class="field">
      <div class="label">Username</div>
      <div class="row" style="cursor:not-allowed">
        <span class="ico">👤</span>
        <input id="u" type="text" placeholder="Username"
               value="{{DEFAULT_USER}}" autocomplete="off" spellcheck="false"/>
        <span class="lock-ico">🔒</span>
      </div>
    </div>
    <div class="field">
      <div class="label">Token / Password</div>
      <div class="row">
        <span class="ico">🔑</span>
        <input id="p" type="password" placeholder="Enter token or password" autocomplete="off" value="{{DEFAULT_PASS}}"/>
        <button class="eye" id="eye" onclick="toggleEye()" tabindex="-1">👁</button>
      </div>
    </div>
    <label class="check-row">
      <input type="checkbox" id="a">
      <span class="check-box"></span>
      <span class="check-label">Apply for all profile</span>
    </label>
  </div>

  <div class="err" id="err"></div>

  <div class="actions">
    <button class="btn-main" id="btn" onclick="go()">Connect</button>
    <div style="height: 4px"></div>
    <button class="btn-cancel" onclick="_onCancel()">Cancel</button>
    <div style="height: 8px"></div> <!-- Extra space below cancel -->
  </div>

<script>
window.addEventListener('load', () => {
  const u = document.getElementById('u');
  const p = document.getElementById('p');
  // Always focus username first as requested, but if it has value, 
  // allow user to see it then they can tab to password or we can focus password after a tiny delay
  u.focus();
  if(u.value) {
    // If username is already there, we focus it but let user tab to password.
    // Or we can just focus password if username is readonly and filled.
    // However, the user specifically asked for focus on username column.
  }
  // Prevent editing username but allow selection/copy
  u.addEventListener('keydown', e => {
    // Allow Cmd+C, Cmd+A, Tab, Arrow keys
    if (e.metaKey || e.ctrlKey || e.key === 'Tab' || e.key.startsWith('Arrow')) return;
    e.preventDefault();
  });
  u.addEventListener('paste', e => e.preventDefault());
  u.addEventListener('cut', e => e.preventDefault());

  // Ensure Cmd+A works specifically for username
  u.addEventListener('keydown', e => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
      u.select();
      e.preventDefault();
    }
  });

  [u, p].forEach(el => {
    el.addEventListener('keydown', e => {
      if (e.key === 'Enter') go();
      if (e.key === 'Escape') _onCancel();
    });
  });
});

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
});

function toggleEye() {
  const p = document.getElementById('p');
  const btn = document.getElementById('eye');
  p.type = p.type === 'password' ? 'text' : 'password';
  btn.textContent = p.type === 'password' ? '👁' : '🙈';
}

async function go() {
  const u = document.getElementById('u').value.trim();
  const p = document.getElementById('p').value;
  const a = document.getElementById('a').checked;
  const err = document.getElementById('err');
  err.classList.remove('on');
  if (!u) { err.textContent='Username is required.'; err.classList.add('on'); document.getElementById('u').focus(); return; }
  if (!p)  { err.textContent='Token / Password is required.'; err.classList.add('on'); document.getElementById('p').focus(); return; }
  const btn = document.getElementById('btn');
  btn.textContent='Connecting…'; btn.disabled=true;
  await _onSubmit(u, p, a);
}
</script>
</body>
</html>`
