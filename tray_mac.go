package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

extern void goTrayClicked();

@interface TrayAction : NSObject
- (void)onClicked:(id)sender;
@end

@implementation TrayAction
- (void)onClicked:(id)sender {
    goTrayClicked();
}
@end

static TrayAction *action;
static NSStatusItem *statusItem;

void setup_native_tray(const char* iconBase64) {
    dispatch_async(dispatch_get_main_queue(), ^{
        statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
        
        NSData *data = [[NSData alloc] initWithBase64EncodedString:[NSString stringWithUTF8String:iconBase64] options:0];
        NSImage *image = [[NSImage alloc] initWithData:data];
        [image setSize:NSMakeSize(18, 18)];
        [image setTemplate:YES];
        
        statusItem.button.image = image;
        
        action = [[TrayAction alloc] init];
        statusItem.button.target = action;
        statusItem.button.action = @selector(onClicked:);
    });
}
*/
import "C"
import (
	"encoding/base64"
	"unsafe"
)

//export goTrayClicked
func goTrayClicked() {
	openDashboard()
}

func startNativeTray() {
	iconStr := base64.StdEncoding.EncodeToString(trayIconData)
	cStr := C.CString(iconStr)
	defer C.free(unsafe.Pointer(cStr))
	C.setup_native_tray(cStr)
	
	// Start Cocoa app loop if not already running
	// Note: webview or systray usually starts this.
	// Since we are replacing systray, we might need to run it ourselves.
	C.CFRunLoopRun()
}
