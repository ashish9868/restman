//go:build darwin && !browser

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework UniformTypeIdentifiers

#import <Cocoa/Cocoa.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>
#include <string.h>

static NSWindow *app_window;
static WKWebView *app_view;

@interface RestmanDelegate : NSObject <NSApplicationDelegate>
@end

@implementation RestmanDelegate
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
	(void)sender;
	return YES;
}
@end

static void install_menus(void) {
	NSMenu *bar = [NSMenu new];
	NSMenuItem *appItem = [NSMenuItem new];
	[bar addItem:appItem];
	NSMenu *appMenu = [NSMenu new];
	NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"Quit Restman"
	                                              action:@selector(terminate:)
	                                       keyEquivalent:@"q"];
	[appMenu addItem:quit];
	[appItem setSubmenu:appMenu];

	NSMenuItem *editItem = [NSMenuItem new];
	[bar addItem:editItem];
	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
	NSArray *actions = @[
		@[@"Undo", @"undo:", @"z"],
		@[@"Redo", @"redo:", @"Z"],
		@[@"Cut", @"cut:", @"x"],
		@[@"Copy", @"copy:", @"c"],
		@[@"Paste", @"paste:", @"v"],
		@[@"Select All", @"selectAll:", @"a"],
	];
	for (NSArray *item in actions) {
		NSMenuItem *entry = [[NSMenuItem alloc] initWithTitle:item[0]
		                                               action:NSSelectorFromString(item[1])
		                                        keyEquivalent:item[2]];
		[edit addItem:entry];
	}
	[editItem setSubmenu:edit];
	[NSApp setMainMenu:bar];
}

static void open_window(const char *url) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		RestmanDelegate *delegate = [RestmanDelegate new];
		[NSApp setDelegate:delegate];
		install_menus();

		NSRect frame = NSMakeRect(0, 0, 1440, 900);
		app_window = [[NSWindow alloc] initWithContentRect:frame
		                                          styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable)
		                                            backing:NSBackingStoreBuffered
		                                              defer:NO];
		[app_window setTitle:@"Restman"];
		[app_window setReleasedWhenClosed:NO];
		[app_window center];
		[app_window setCollectionBehavior:NSWindowCollectionBehaviorFullScreenPrimary];

		WKWebViewConfiguration *config = [WKWebViewConfiguration new];
		app_view = [[WKWebView alloc] initWithFrame:frame configuration:config];
		[app_window setContentView:app_view];
		NSURL *target = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
		[app_view loadRequest:[NSURLRequest requestWithURL:target]];
		[app_window makeKeyAndOrderFront:nil];
		[NSApp activateIgnoringOtherApps:YES];
		[NSApp run];
	}
}

static void set_title(const char *title) {
	NSString *value = title ? [NSString stringWithUTF8String:title] : @"Restman";
	dispatch_async(dispatch_get_main_queue(), ^{
		[app_window setTitle:value];
	});
}

static void set_zoom(double level) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (app_view) {
			app_view.pageZoom = level;
		}
	});
}

static char *save_file(const char *name) {
	NSString *suggested = name ? [NSString stringWithUTF8String:name] : @"";
	__block char *result = NULL;
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSSavePanel *panel = [NSSavePanel savePanel];
		panel.nameFieldStringValue = suggested;
		panel.allowedContentTypes = @[UTTypeJSON];
		if ([panel runModal] == NSModalResponseOK && panel.URL.path != nil) {
			result = strdup(panel.URL.path.UTF8String);
		}
	});
	return result;
}

static char *pick_file(void) {
	__block char *result = NULL;
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSOpenPanel *panel = [NSOpenPanel openPanel];
		panel.canChooseFiles = YES;
		panel.canChooseDirectories = NO;
		panel.allowsMultipleSelection = NO;
		if ([panel runModal] == NSModalResponseOK && panel.URL.path != nil) {
			result = strdup(panel.URL.path.UTF8String);
		}
	});
	return result;
}
*/
import "C"
import (
	"fmt"
	"log"
	"net"
	"net/http"
	"unsafe"
)

func serve(listener net.Listener, handler http.Handler, page string) error {
	go func() {
		if err := http.Serve(listener, handler); err != nil {
			log.Fatal(err)
		}
	}()
	fmt.Println("Opening Restman")
	target := C.CString(page)
	defer C.free(unsafe.Pointer(target))
	C.open_window(target)
	return nil
}

func setWindowTitle(title string) {
	if title == "" {
		title = "Restman"
	}
	value := C.CString(title)
	defer C.free(unsafe.Pointer(value))
	C.set_title(value)
}

func setZoom(level float64) {
	if level < 0.5 {
		level = 0.5
	}
	if level > 2 {
		level = 2
	}
	C.set_zoom(C.double(level))
}

func saveFile(name string) (string, error) {
	suggested := C.CString(name)
	path := C.save_file(suggested)
	C.free(unsafe.Pointer(suggested))
	if path == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(path))
	return C.GoString(path), nil
}

func pickFile() (string, error) {
	path := C.pick_file()
	if path == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(path))
	return C.GoString(path), nil
}
