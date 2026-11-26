#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

extern char *goCall(char *request);

extern int goEnglish(void);

// Translates Cocoa strings; the rest is translated in Go and ui.html.
static NSString *T(NSString *uk, NSString *en) { return goEnglish() ? en : uk; }

static NSMenu *addMenu(NSMenu *bar, NSString *title) {
    NSMenuItem *item = [bar addItemWithTitle:title action:nil keyEquivalent:@""];
    item.submenu = [[NSMenu alloc] initWithTitle:title];
    return item.submenu;
}

// Without a menu Cmd+Q, Cmd+C etc. do not work.
static void buildMenu(void) {
    NSMenu *bar = [NSMenu new];
    NSMenu *main = addMenu(bar, @"macOSStat");
    [main addItemWithTitle:T(@"Завершити macOSStat", @"Quit macOSStat") action:@selector(terminate:) keyEquivalent:@"q"];
    NSMenu *edit = addMenu(bar, T(@"Редагування", @"Edit"));
    [edit addItemWithTitle:T(@"Вирізати", @"Cut") action:@selector(cut:) keyEquivalent:@"x"];
    [edit addItemWithTitle:T(@"Скопіювати", @"Copy") action:@selector(copy:) keyEquivalent:@"c"];
    [edit addItemWithTitle:T(@"Вставити", @"Paste") action:@selector(paste:) keyEquivalent:@"v"];
    [edit addItemWithTitle:T(@"Вибрати все", @"Select All") action:@selector(selectAll:) keyEquivalent:@"a"];
    NSMenu *win = addMenu(bar, T(@"Вікно", @"Window"));
    [win addItemWithTitle:T(@"Закрити", @"Close") action:@selector(performClose:) keyEquivalent:@"w"];
    NSApp.mainMenu = bar;
}

static NSString *call(NSString *request) {
    char *raw = goCall((char *)request.UTF8String);
    NSString *json = [NSString stringWithUTF8String:raw];
    free(raw);
    return json;
}

@interface AppDelegate : NSObject <NSApplicationDelegate, WKScriptMessageHandlerWithReply, NSMenuDelegate>
@property(strong) NSWindow *window;
@property(copy) NSString *html;
@property(strong) NSStatusItem *status;
@property(strong) NSTimer *timer;
@property(copy) NSArray<NSString *> *lines;
@property BOOL polling;
@end

@implementation AppDelegate

- (void)applicationDidFinishLaunching:(NSNotification *)note {
    WKWebViewConfiguration *config = [WKWebViewConfiguration new];
    [config.userContentController addScriptMessageHandlerWithReply:self
                                                      contentWorld:WKContentWorld.pageWorld
                                                              name:@"snapshot"];
    NSRect frame = NSMakeRect(0, 0, 1160, 800);
    WKWebView *web = [[WKWebView alloc] initWithFrame:frame configuration:config];

    self.window = [[NSWindow alloc]
        initWithContentRect:frame
                  styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                            NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
                    backing:NSBackingStoreBuffered
                      defer:NO];
    self.window.releasedWhenClosed = NO;
    self.window.title = @"macOSStat";
    self.window.minSize = NSMakeSize(620, 440);
    self.window.contentView = web;
    [self.window center];
    [self.window makeKeyAndOrderFront:nil];
    [web loadHTMLString:self.html baseURL:nil];
    [NSApp activateIgnoringOtherApps:YES];

    self.status = [NSStatusBar.systemStatusBar statusItemWithLength:NSVariableStatusItemLength];
    self.status.button.title = @"macOSStat";
    self.status.button.font = [NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightRegular];
    self.status.menu = [NSMenu new];
    self.status.menu.autoenablesItems = NO;
    self.status.menu.delegate = self;
    self.timer = [NSTimer scheduledTimerWithTimeInterval:5 target:self selector:@selector(poll) userInfo:nil repeats:YES];
    [self poll];
}

- (void)poll {
    if (self.polling) return;
    self.polling = YES;
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        NSData *data = [call(@"{\"op\":\"menu\"}") dataUsingEncoding:NSUTF8StringEncoding];
        NSDictionary *info = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
        dispatch_async(dispatch_get_main_queue(), ^{
            self.polling = NO;
            if (![info[@"title"] isKindOfClass:NSString.class]) return;
            self.status.button.title = info[@"title"];
            self.lines = [info[@"lines"] isKindOfClass:NSArray.class] ? info[@"lines"] : @[];
        });
    });
}

- (void)menuNeedsUpdate:(NSMenu *)menu {
    [menu removeAllItems];
    // Data rows are regular items so they are drawn at full contrast; a click opens the window.
    for (NSString *line in self.lines)
        [menu addItemWithTitle:line action:@selector(showWindow) keyEquivalent:@""].target = self;
    [menu addItem:NSMenuItem.separatorItem];
    [menu addItemWithTitle:T(@"Відкрити macOSStat", @"Open macOSStat") action:@selector(showWindow) keyEquivalent:@""].target = self;
    [menu addItemWithTitle:T(@"Завершити", @"Quit") action:@selector(terminate:) keyEquivalent:@""].target = NSApp;
}

- (void)showWindow {
    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}

- (BOOL)applicationShouldHandleReopen:(NSApplication *)app hasVisibleWindows:(BOOL)visible {
    if (!visible) [self showWindow];
    return YES;
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)app {
    return NO; // stays in the menu bar
}

- (void)userContentController:(WKUserContentController *)controller
      didReceiveScriptMessage:(WKScriptMessage *)message
                 replyHandler:(void (^)(id, NSString *))reply {
    NSString *request = [message.body isKindOfClass:NSString.class] ? message.body : @"";
    int wasEnglish = goEnglish();
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        NSString *json = call(request);
        dispatch_async(dispatch_get_main_queue(), ^{
            if (goEnglish() != wasEnglish) {
                buildMenu();
                [self poll];
            }
            if (json) reply(json, nil);
            else reply(nil, T(@"некоректні дані sysctl", @"invalid sysctl data"));
        });
    });
}

@end

void runWindow(const char *html, const void *icon, int iconLen) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        app.activationPolicy = NSApplicationActivationPolicyRegular;

        app.applicationIconImage = [[NSImage alloc] initWithData:[NSData dataWithBytes:icon length:iconLen]];
        buildMenu();

        AppDelegate *delegate = [AppDelegate new];
        delegate.html = [NSString stringWithUTF8String:html];
        app.delegate = delegate; // weak reference; delegate lives until run returns
        [app run];
    }
}
