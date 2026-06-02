package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>

void login_frame(void *w) {
    NSWindow *window = (NSWindow *)w;
    [window setStyleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskFullSizeContentView];
    [window setTitleVisibility:NSWindowTitleHidden];
    [window setTitlebarAppearsTransparent:YES];
    [window setMovableByWindowBackground:YES];
    [window setHasShadow:YES];
    [window setLevel:NSFloatingWindowLevel];
    NSPoint mouseLoc = [NSEvent mouseLocation];
    NSScreen *screen = [NSScreen mainScreen];
    NSRect visibleFrame = [screen visibleFrame];
    NSRect windowFrame = [window frame];
    CGFloat x = mouseLoc.x - (windowFrame.size.width/2);
    if (x < visibleFrame.origin.x) x = visibleFrame.origin.x + 10;
    if (x + windowFrame.size.width > visibleFrame.origin.x + visibleFrame.size.width) x = visibleFrame.origin.x + visibleFrame.size.width - windowFrame.size.width - 10;
    CGFloat y = visibleFrame.origin.y + visibleFrame.size.height - windowFrame.size.height - 5;
    [window setFrameOrigin:NSMakePoint(x, y)];
}

void login_glass(void *w) {
    NSWindow *window = (NSWindow *)w;
    window.appearance = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    window.backgroundColor = [NSColor colorWithRed:0.098 green:0.106 blue:0.110 alpha:0.95];
    [window.contentView setWantsLayer:YES];
    window.contentView.layer.cornerRadius = 12;
    window.contentView.layer.masksToBounds = YES;
}

void login_focus(void *w) {
    NSWindow *window = (NSWindow *)w;
    [NSApp activateIgnoringOtherApps:YES];
    [window makeKeyAndOrderFront:nil];
}

void login_menu() {
    NSMenu *mainMenu = [NSApp mainMenu];
    if (mainMenu == nil) { mainMenu = [[NSMenu alloc] init]; [NSApp setMainMenu:mainMenu]; }
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    [[editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"] setKeyEquivalentModifierMask:NSEventModifierFlagCommand];
    [[editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"] setKeyEquivalentModifierMask:NSEventModifierFlagCommand];
    [[editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"] setKeyEquivalentModifierMask:NSEventModifierFlagCommand];
    [[editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"] setKeyEquivalentModifierMask:NSEventModifierFlagCommand];
    NSMenuItem *editMenuItem = [[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
    [editMenuItem setSubmenu:editMenu];
    [mainMenu addItem:editMenuItem];
}
*/
import "C"

import (
	_ "embed"
	"encoding/base64"
	"strings"
	"sync"

	"github.com/webview/webview_go"
)

//go:embed login_template.html
var loginTemplate string

type loginResult struct {
	user        string
	password    string
	applyAll    bool
	saveProfile bool
	saveUser    bool
	savePass    bool
	canceled    bool
}

func showLoginWindow(profile string, defaultUser string, defaultPass string, su, sp, a, f bool) loginResult {
	result := loginResult{canceled: true}
	var wg sync.WaitGroup
	wg.Add(1)
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("RCP Light")
	w.SetSize(360, 440, webview.HintFixed)
	C.login_frame(w.Window())
	C.login_menu()
	w.Bind("_onSubmit", func(user string, password string, applyAll bool, saveProfile bool, saveUser bool, savePass bool) {
		result = loginResult{user: user, password: password, applyAll: applyAll, saveProfile: saveProfile, saveUser: saveUser, savePass: savePass, canceled: false}
		w.Terminate()
		wg.Done()
	})
	w.Bind("_onCancel", func() { result = loginResult{canceled: true}; w.Terminate(); wg.Done() })
	w.SetHtml(loginHTML(profile, defaultUser, defaultPass, su, sp, a, f))
	C.login_glass(w.Window())
	C.login_focus(w.Window())
	w.Run()
	wg.Wait()
	return result
}

func loginHTML(profile, defaultUser string, defaultPass string, su, sp, a, f bool) string {
	iconBase64 := base64.StdEncoding.EncodeToString(iconData)
	html := strings.ReplaceAll(loginTemplate, "{{PROFILE}}", profile)
	html = strings.ReplaceAll(html, "{{DEFAULT_USER}}", defaultUser)
	html = strings.ReplaceAll(html, "{{DEFAULT_PASS}}", defaultPass)
	html = strings.ReplaceAll(html, "{{ICON_BASE64}}", iconBase64)
	if su { html = strings.ReplaceAll(html, `id="su">`, `id="su" checked>`) }
	if sp { html = strings.ReplaceAll(html, `id="sp">`, `id="sp" checked>`) }
	if a  { html = strings.ReplaceAll(html, `id="a">`,  `id="a" checked>`)  }
	if f  { html = strings.ReplaceAll(html, `id="f">`,  `id="f" checked>`)  }
	return html
}
