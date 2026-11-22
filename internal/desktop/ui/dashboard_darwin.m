//go:build desktop && darwin

#import <Cocoa/Cocoa.h>
#import "dashboard_darwin.h"

// A public NSMenuItem custom view keeps AppKit's native menu navigation and
// systray's existing actions. No dependency internals or application delegate
// replacement are needed. All view ownership and updates stay on the main loop.

static BOOL GSDark(NSAppearance *appearance) {
    return [[appearance bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua, NSAppearanceNameDarkAqua]] isEqualToString:NSAppearanceNameDarkAqua];
}
static NSColor *GSReceiveColor(void) {
    return [NSColor colorWithName:@"GoSyncReceive" dynamicProvider:^NSColor *(NSAppearance *appearance) {
        return GSDark(appearance) ? [NSColor colorWithSRGBRed:0.40 green:0.88 blue:0.74 alpha:1] : [NSColor colorWithSRGBRed:0.04 green:0.43 blue:0.34 alpha:1];
    }];
}
static NSColor *GSSendColor(void) {
    return [NSColor colorWithName:@"GoSyncSend" dynamicProvider:^NSColor *(NSAppearance *appearance) {
        return GSDark(appearance) ? [NSColor colorWithSRGBRed:0.72 green:0.67 blue:0.96 alpha:1] : [NSColor colorWithSRGBRed:0.43 green:0.32 blue:0.70 alpha:1];
    }];
}

static NSTextField *GSLabel(NSView *parent, NSRect frame, CGFloat size, NSFontWeight weight, NSColor *color) {
    NSTextField *label = [NSTextField labelWithString:@""];
    label.frame = frame;
    label.font = [NSFont systemFontOfSize:size weight:weight];
    label.textColor = color;
    label.lineBreakMode = NSLineBreakByTruncatingTail;
    label.maximumNumberOfLines = 1;
    [parent addSubview:label];
    return label;
}

@implementation GSActivityGraph
- (BOOL)isFlipped { return YES; }
- (BOOL)isAccessibilityElement { return YES; }
- (NSString *)accessibilityRole { return NSAccessibilityImageRole; }
- (NSString *)accessibilityLabel {
    return @"Payload transfer activity over the last 60 seconds. Receive is a solid teal line; send is a dashed purple line. Missing measurements leave gaps.";
}
- (void)drawSeries:(NSString *)key color:(NSColor *)color dashed:(BOOL)dashed {
    NSBezierPath *line = [NSBezierPath bezierPath];
    BOOL connected = NO;
    double lastTime = 0;
    CGFloat height = self.bounds.size.height - 4;
    for (NSDictionary *point in self.points) {
        double time = [point[@"time"] doubleValue];
        if (![point[@"valid"] boolValue]) { connected = NO; continue; }
        CGFloat x = MAX(0, MIN(1, (time - (self.now - 60000)) / 60000)) * self.bounds.size.width;
        CGFloat y = 2 + height * (1 - [point[key] doubleValue] / MAX(1, self.peak));
        NSPoint p = NSMakePoint(x, y);
        if (connected && time - lastTime <= 5000) { [line lineToPoint:p]; }
        else { [line moveToPoint:p]; }
        connected = YES;
        lastTime = time;
    }
    line.lineWidth = 1.75;
    line.lineJoinStyle = NSLineJoinStyleRound;
    line.lineCapStyle = NSLineCapStyleRound;
    if (dashed) { CGFloat dash[] = {3, 3}; [line setLineDash:dash count:2 phase:0]; }
    [[color colorWithAlphaComponent:self.fresh ? 1 : 0.45] setStroke];
    [line stroke];
}
- (void)drawRect:(NSRect)dirtyRect {
    [super drawRect:dirtyRect];
    NSBezierPath *grid = [NSBezierPath bezierPath];
    for (NSInteger row = 0; row < 3; row++) {
        CGFloat y = 2 + row * (self.bounds.size.height - 4) / 2;
        [grid moveToPoint:NSMakePoint(0, y)];
        [grid lineToPoint:NSMakePoint(self.bounds.size.width, y)];
    }
    for (NSInteger column = 0; column < 5; column++) {
        CGFloat x = column * self.bounds.size.width / 4;
        [grid moveToPoint:NSMakePoint(x, 2)];
        [grid lineToPoint:NSMakePoint(x, self.bounds.size.height - 2)];
    }
    [[NSColor.separatorColor colorWithAlphaComponent:0.35] setStroke];
    grid.lineWidth = 0.5;
    [grid stroke];
    [self drawSeries:@"receive" color:GSReceiveColor() dashed:NO];
    [self drawSeries:@"send" color:GSSendColor() dashed:YES];
}
@end

@interface GSDashboard : NSView
@property NSTextField *health;
@property NSTextField *security;
@property NSTextField *folder;
@property NSTextField *location;
@property NSTextField *peers;
@property NSTextField *files;
@property NSTextField *transfers;
@property NSTextField *receive;
@property NSTextField *send;
@property NSTextField *detail;
@property NSTextField *graphCaption;
@property NSImageView *healthIcon;
@property GSActivityGraph *graph;
- (void)update:(NSDictionary *)data;
@end

@implementation GSDashboard
- (BOOL)isFlipped { return YES; }
- (instancetype)init {
    self = [super initWithFrame:NSMakeRect(0, 0, 344, 344)];
    if (!self) return nil;
    _healthIcon = [[NSImageView alloc] initWithFrame:NSMakeRect(17, 17, 27, 27)];
    [self addSubview:_healthIcon];
    GSLabel(self, NSMakeRect(55, 12, 130, 25), 18, NSFontWeightSemibold, NSColor.labelColor).stringValue = @"GoSync";
    _health = GSLabel(self, NSMakeRect(55, 35, 272, 19), 12, NSFontWeightMedium, NSColor.secondaryLabelColor);
    _security = GSLabel(self, NSMakeRect(205, 17, 122, 18), 10, NSFontWeightMedium, NSColor.secondaryLabelColor);
    _security.alignment = NSTextAlignmentRight;
    _folder = GSLabel(self, NSMakeRect(29, 72, 286, 20), 13, NSFontWeightSemibold, NSColor.labelColor);
    _location = GSLabel(self, NSMakeRect(29, 92, 286, 15), 10, NSFontWeightRegular, NSColor.secondaryLabelColor);
    _location.lineBreakMode = NSLineBreakByTruncatingMiddle;
    _peers = GSLabel(self, NSMakeRect(17, 124, 104, 25), 19, NSFontWeightMedium, NSColor.labelColor);
    _files = GSLabel(self, NSMakeRect(128, 124, 104, 25), 19, NSFontWeightMedium, NSColor.labelColor);
    _transfers = GSLabel(self, NSMakeRect(239, 124, 88, 25), 19, NSFontWeightMedium, NSColor.labelColor);
    NSArray *metrics = @[_peers, _files, _transfers];
    NSArray *names = @[@"RECENT PEERS", @"ENTRIES", @"TRANSFERS"];
    NSArray *accessibility = @[@"Recently reachable peers out of known peers", @"Tracked entries, including directories", @"Active transfers"];
    for (NSUInteger i = 0; i < metrics.count; i++) {
        NSTextField *metric = metrics[i];
        metric.font = [NSFont monospacedDigitSystemFontOfSize:19 weight:NSFontWeightMedium];
        metric.accessibilityLabel = accessibility[i];
        metric.toolTip = accessibility[i];
        GSLabel(self, NSMakeRect(metric.frame.origin.x, 152, metric.frame.size.width, 15), 9, NSFontWeightMedium, NSColor.secondaryLabelColor).stringValue = names[i];
    }
    GSLabel(self, NSMakeRect(17, 185, 145, 16), 10, NSFontWeightMedium, GSReceiveColor()).stringValue = @"↓  RECEIVE";
    GSLabel(self, NSMakeRect(183, 185, 144, 16), 10, NSFontWeightMedium, GSSendColor()).stringValue = @"↑  SEND";
    _receive = GSLabel(self, NSMakeRect(17, 204, 151, 25), 18, NSFontWeightMedium, NSColor.labelColor);
    _send = GSLabel(self, NSMakeRect(183, 204, 144, 25), 18, NSFontWeightMedium, NSColor.labelColor);
    _receive.font = _send.font = [NSFont monospacedDigitSystemFontOfSize:18 weight:NSFontWeightMedium];
    _receive.accessibilityLabel = @"Received payload per second";
    _send.accessibilityLabel = @"Sent payload per second";
    _graph = [[GSActivityGraph alloc] initWithFrame:NSMakeRect(17, 240, 310, 50)];
    [self addSubview:_graph];
    GSLabel(self, NSMakeRect(17, 295, 95, 14), 9, NSFontWeightRegular, NSColor.secondaryLabelColor).stringValue = @"60 seconds ago";
    _graphCaption = GSLabel(self, NSMakeRect(112, 295, 215, 14), 9, NSFontWeightRegular, NSColor.secondaryLabelColor);
    _graphCaption.alignment = NSTextAlignmentRight;
    _detail = GSLabel(self, NSMakeRect(17, 319, 310, 17), 10, NSFontWeightRegular, NSColor.secondaryLabelColor);
    [self update:@{@"title": @"Loading…", @"folder": @"Choose a folder", @"location": @"Open Settings to get started"}];
    return self;
}
- (void)drawRect:(NSRect)dirtyRect {
    [super drawRect:dirtyRect];
    [[NSColor.quaternaryLabelColor colorWithAlphaComponent:0.08] setFill];
    [[NSBezierPath bezierPathWithRoundedRect:NSMakeRect(17, 65, 310, 49) xRadius:7 yRadius:7] fill];
    [NSColor.separatorColor setStroke];
    NSBezierPath *line = [NSBezierPath bezierPath];
    [line moveToPoint:NSMakePoint(17, 176)];
    [line lineToPoint:NSMakePoint(327, 176)];
    line.lineWidth = 0.5;
    [line stroke];
}
- (void)update:(NSDictionary *)data {
    _health.stringValue = data[@"title"] ?: @"Status unavailable";
    _security.stringValue = data[@"security"] ?: @"";
    _security.toolTip = [_security.stringValue isEqualToString:@"Open LAN"] ? @"Unauthenticated, unencrypted LAN" : _security.stringValue;
    _folder.stringValue = data[@"folder"] ?: @"Choose a folder";
    _location.stringValue = data[@"location"] ?: @"";
    _folder.toolTip = _location.toolTip = data[@"fullPath"];
    _folder.accessibilityLabel = @"Synchronization folder";
    _location.accessibilityValue = data[@"fullPath"] ?: @"No folder selected";
    _peers.stringValue = data[@"peers"] ?: @"—";
    _files.stringValue = data[@"files"] ?: @"—";
    _transfers.stringValue = data[@"transfers"] ?: @"—";
    _receive.stringValue = data[@"receive"] ?: @"—";
    _send.stringValue = data[@"send"] ?: @"—";
    NSString *health = data[@"health"];
    NSColor *color = NSColor.secondaryLabelColor;
    NSString *symbol = @"circle.dotted";
    if ([health isEqualToString:@"idle"]) { color = GSReceiveColor(); symbol = @"checkmark.circle.fill"; }
    else if ([health isEqualToString:@"syncing"]) { color = GSReceiveColor(); symbol = @"arrow.triangle.2.circlepath"; }
    else if ([health isEqualToString:@"warning"]) { color = NSColor.systemOrangeColor; symbol = @"exclamationmark.triangle.fill"; }
    else if ([health isEqualToString:@"error"]) { color = NSColor.systemRedColor; symbol = @"xmark.circle.fill"; }
    else if ([health isEqualToString:@"stopped"]) { symbol = @"stop.circle"; }
    _healthIcon.image = [NSImage imageWithSystemSymbolName:symbol accessibilityDescription:nil];
    _healthIcon.contentTintColor = color;
    _health.textColor = color;
    _detail.stringValue = data[@"summary"] ?: @"";
    _detail.toolTip = data[@"detail"] ?: @"";
    _health.toolTip = _detail.toolTip;
    _graph.points = [data[@"points"] isKindOfClass:NSArray.class] ? data[@"points"] : @[];
    _graph.now = [data[@"now"] doubleValue];
    _graph.fresh = [data[@"fresh"] boolValue];
    _graph.peak = 0;
    BOOL measured = NO;
    for (NSDictionary *point in _graph.points) {
        if ([point[@"valid"] boolValue]) {
            measured = YES;
            _graph.peak = MAX(_graph.peak, MAX([point[@"receive"] doubleValue], [point[@"send"] doubleValue]));
        }
    }
    NSString *peak = _graph.peak >= 1e6 ? [NSString stringWithFormat:@"%.1f MB/s", _graph.peak / 1e6] :
        _graph.peak >= 1e3 ? [NSString stringWithFormat:@"%.1f kB/s", _graph.peak / 1e3] : [NSString stringWithFormat:@"%.0f B/s", _graph.peak];
    _graphCaption.stringValue = !measured ? @"Waiting for measurements" : !_graph.fresh ? @"Historical · status unavailable" : [NSString stringWithFormat:@"Peak %@ · now", peak];
    _graph.accessibilityValue = _graphCaption.stringValue;
    _graph.needsDisplay = YES;
    self.needsDisplay = YES;
}
@end

static GSDashboard *dashboard;
static NSDictionary *latest;
static id menuObserver;

static void GSDecorateMenu(NSMenu *menu) {
    if (menu.numberOfItems == 0 || ![menu.itemArray.firstObject.title hasPrefix:@"GoSync · "]) return;
    menu.minimumWidth = 344;
    if (!dashboard) {
        dashboard = [[GSDashboard alloc] init];
        menu.itemArray.firstObject.view = dashboard;
        if (latest) [dashboard update:latest];
    }
    NSDictionary *symbols = @{
        @"Open Sync Folder": @"folder", @"Rescan Now": @"arrow.clockwise",
        @"Status, Peers & Transfers…": @"chart.xyaxis.line", @"Start GoSync": @"play",
        @"Stop GoSync": @"stop", @"Settings…": @"slider.horizontal.3",
        @"View Logs…": @"doc.text.magnifyingglass", @"Launch at Login": @"power",
        @"Quit Tray · keep syncing": @"rectangle.portrait.and.arrow.right"
    };
    NSDictionary *keys = @{@"Open Sync Folder": @"o", @"Rescan Now": @"r", @"Settings…": @",", @"Quit Tray · keep syncing": @"q"};
    for (NSMenuItem *item in menu.itemArray) {
        NSString *symbol = symbols[item.title];
        if (!symbol) continue;
        NSImage *image = [NSImage imageWithSystemSymbolName:symbol accessibilityDescription:nil];
        image = [image imageWithSymbolConfiguration:[NSImageSymbolConfiguration configurationWithPointSize:13 weight:NSFontWeightRegular]];
        image.template = YES;
        item.image = image;
        if (keys[item.title]) { item.keyEquivalent = keys[item.title]; item.keyEquivalentModifierMask = NSEventModifierFlagCommand; }
    }
}

void GSPrepareDashboard(void) {
    void (^prepare)(void) = ^{
        // Registration precedes systray's first queued menu insertion. Observing
        // additions also styles later items without intercepting menu delegates.
        menuObserver = [NSNotificationCenter.defaultCenter addObserverForName:NSMenuDidAddItemNotification object:nil queue:nil usingBlock:^(NSNotification *notification) {
            GSDecorateMenu(notification.object);
        }];
    };
    if (NSThread.isMainThread) prepare(); else dispatch_sync(dispatch_get_main_queue(), prepare);
}

void GSUpdateDashboard(const char *json) {
    NSData *bytes = [[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    NSDictionary *data = [NSJSONSerialization JSONObjectWithData:bytes options:0 error:nil];
    if (![data isKindOfClass:NSDictionary.class]) return;
    // Common run-loop modes keep the graph updating while the menu tracks input.
    CFRunLoopPerformBlock(CFRunLoopGetMain(), kCFRunLoopCommonModes, ^{
        latest = data;
        [dashboard update:data];
    });
    CFRunLoopWakeUp(CFRunLoopGetMain());
}
