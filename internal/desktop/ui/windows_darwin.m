//go:build desktop && darwin

#import <Cocoa/Cocoa.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import "dashboard_darwin.h"

extern void GSDispatchWindowAction(const char *action);

@interface GSWindowMenuActions : NSObject
@end
@implementation GSWindowMenuActions
- (void)perform:(NSMenuItem *)sender { GSDispatchWindowAction([sender.representedObject UTF8String]); }
@end
static GSWindowMenuActions *windowMenuActions;

void GSPrepareWindows(void) {
    dispatch_sync(dispatch_get_main_queue(), ^{
        windowMenuActions = [[GSWindowMenuActions alloc] init];
        NSMenu *main = [[NSMenu alloc] init];
        NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"GoSync"];
        NSMenuItem *appItem = [[NSMenuItem alloc] initWithTitle:@"GoSync" action:nil keyEquivalent:@""];
        appItem.submenu = appMenu; [main addItem:appItem];
        for (NSArray *spec in @[@[@"Settings…",@",",@"settings"],@[@"Activity",@"1",@"details"],@[@"Quit Tray · keep syncing",@"q",@"quit"]]) {
            NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:spec[0] action:@selector(perform:) keyEquivalent:spec[1]];
            item.target = windowMenuActions; item.representedObject = spec[2]; [appMenu addItem:item];
        }
        NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
        NSMenuItem *editItem = [[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
        editItem.submenu = edit; [main addItem:editItem];
        [edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
        [edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
        [edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
        NSMenu *windows = [[NSMenu alloc] initWithTitle:@"Window"];
        NSMenuItem *windowItem = [[NSMenuItem alloc] initWithTitle:@"Window" action:nil keyEquivalent:@""];
        windowItem.submenu = windows; [main addItem:windowItem];
        [windows addItemWithTitle:@"Close Window" action:@selector(performClose:) keyEquivalent:@"w"];
        [windows addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
        NSApp.mainMenu = main;
        NSApp.windowsMenu = windows;
    });
}

@interface GSFlippedView : NSView
@end
@implementation GSFlippedView
- (BOOL)isFlipped { return YES; }
@end

@interface GSCard : GSFlippedView
@end
@implementation GSCard
- (void)drawRect:(NSRect)rect {
    [NSColor.controlBackgroundColor setFill];
    NSBezierPath *path = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(self.bounds, .5, .5) xRadius:10 yRadius:10];
    [path fill];
    [[NSColor.separatorColor colorWithAlphaComponent:.45] setStroke];
    path.lineWidth = .5;
    [path stroke];
}
- (void)viewDidChangeEffectiveAppearance { [super viewDidChangeEffectiveAppearance]; self.needsDisplay = YES; }
@end

static NSTextField *GSText(NSView *parent, NSRect frame, NSString *text, CGFloat size, NSFontWeight weight, BOOL secondary) {
    NSTextField *field = [NSTextField labelWithString:text];
    field.frame = frame;
    field.font = [NSFont systemFontOfSize:size weight:weight];
    field.textColor = secondary ? NSColor.secondaryLabelColor : NSColor.labelColor;
    field.lineBreakMode = NSLineBreakByTruncatingTail;
    field.maximumNumberOfLines = 1;
    [parent addSubview:field];
    return field;
}
static void GSSetText(NSTextField *field, NSString *text) {
    field.stringValue = text ?: @"";
    field.toolTip = text;
}
static GSCard *GSAddCard(NSView *parent, NSRect frame) {
    GSCard *card = [[GSCard alloc] initWithFrame:frame];
    [parent addSubview:card];
    return card;
}
static NSButton *GSButton(NSView *parent, NSRect frame, NSString *title, NSString *identifier, id target, SEL selector) {
    NSButton *button = [NSButton buttonWithTitle:title target:target action:selector];
    button.frame = frame;
    button.bezelStyle = NSBezelStyleRounded;
    button.identifier = identifier;
    button.accessibilityIdentifier = identifier;
    [parent addSubview:button];
    return button;
}
static NSWindow *GSWindow(NSString *title, NSSize size) {
    NSWindow *window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,size.width,size.height)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable
        backing:NSBackingStoreBuffered defer:NO];
    window.title = title;
    window.releasedWhenClosed = NO;
    window.contentView = [[GSFlippedView alloc] initWithFrame:NSMakeRect(0,0,size.width,size.height)];
    window.backgroundColor = NSColor.windowBackgroundColor;
    window.collectionBehavior = NSWindowCollectionBehaviorMoveToActiveSpace;
    [window center];
    return window;
}
static void GSFront(NSWindow *window) {
    [NSApp activateIgnoringOtherApps:YES];
    if (window.miniaturized) [window deminiaturize:nil];
    [window makeKeyAndOrderFront:nil];
}
static NSString *GSString(NSDictionary *data, NSString *key) {
    id value = data[key];
    return [value isKindOfClass:NSString.class] ? value : @"";
}
static NSPathControl *GSPath(NSView *parent, NSRect frame, NSString *placeholder) {
    NSPathControl *path = [[NSPathControl alloc] initWithFrame:frame];
    path.pathStyle = NSPathStyleStandard;
    path.placeholderString = placeholder;
    path.editable = NO;
    [parent addSubview:path];
    return path;
}
static void GSSetPath(NSPathControl *control, NSString *path) {
    NSURL *url = path.length ? [NSURL fileURLWithPath:path] : nil;
    if (![control.URL isEqual:url]) control.URL = url;
    control.toolTip = path;
    control.accessibilityValue = path.length ? path : control.placeholderString;
}

@interface GSSettingsController : NSWindowController
@property NSDictionary *data;
@property NSTextField *folderName, *folderHint, *health, *security, *feedback;
@property NSPathControl *folderPath, *configPath;
@property NSButton *chooseFolder, *chooseConfig, *startStop, *login, *trayLogin, *reveal, *remove;
- (void)update:(NSDictionary *)data;
@end

@implementation GSSettingsController
- (instancetype)init {
    self = [super initWithWindow:GSWindow(@"GoSync Settings", NSMakeSize(640, 600))];
    if (!self) return nil;
    NSView *root = self.window.contentView;
    GSText(root, NSMakeRect(24,18,480,32), @"Settings", 25, NSFontWeightBold, NO);
    GSText(root, NSMakeRect(24,53,580,18), @"Choose what syncs and when GoSync runs.", 12, NSFontWeightRegular, YES);
    GSButton(root,NSMakeRect(486,21,130,30),@"View Activity",@"details",self,@selector(sendAction:));

    NSView *folder = GSAddCard(root,NSMakeRect(24,88,592,126));
    NSImageView *icon = [[NSImageView alloc] initWithFrame:NSMakeRect(18,20,26,26)];
    icon.image = [NSImage imageWithSystemSymbolName:@"folder" accessibilityDescription:nil];
    icon.contentTintColor = NSColor.controlAccentColor;
    [folder addSubview:icon];
    _folderName = GSText(folder,NSMakeRect(56,20,338,26),@"Choose your sync folder",16,NSFontWeightSemibold,NO);
    _chooseFolder = GSButton(folder,NSMakeRect(410,18,164,30),@"Choose Folder…",@"choose-folder",self,@selector(pickFolder:));
    _folderPath = GSPath(folder,NSMakeRect(18,58,556,24),@"No folder selected");
    _folderPath.accessibilityLabel = @"Synchronization folder";
    _folderHint = GSText(folder,NSMakeRect(20,94,552,17),@"Use a new folder, or import an existing configuration below.",11,NSFontWeightRegular,YES);

    NSView *service = GSAddCard(root,NSMakeRect(24,230,592,172));
    GSText(service,NSMakeRect(20,16,330,22),@"Synchronization",14,NSFontWeightSemibold,NO);
    _health = GSText(service,NSMakeRect(20,42,410,19),@"Not configured",12,NSFontWeightRegular,YES);
    _startStop = GSButton(service,NSMakeRect(456,16,116,30),@"Start",@"start-stop",self,@selector(startStop:));
    _login = [NSButton checkboxWithTitle:@"Start synchronization at login" target:self action:@selector(sendAction:)];
    _login.frame = NSMakeRect(20,76,550,24); _login.identifier = @"login";
    [service addSubview:_login];
    _trayLogin = [NSButton checkboxWithTitle:@"Show GoSync in the menu bar at login" target:self action:@selector(sendAction:)];
    _trayLogin.frame = NSMakeRect(20,108,550,24); _trayLogin.identifier = @"tray-login";
    [service addSubview:_trayLogin];
    _security = GSText(service,NSMakeRect(20,145,550,17),@"Connection mode appears after selecting a folder.",11,NSFontWeightRegular,YES);

    NSView *config = GSAddCard(root,NSMakeRect(24,418,592,108));
    GSText(config,NSMakeRect(20,16,300,22),@"Existing configuration",14,NSFontWeightSemibold,NO);
    _chooseConfig = GSButton(config,NSMakeRect(406,12,168,30),@"Use Existing…",@"choose-config",self,@selector(pickConfig:));
    _configPath = GSPath(config,NSMakeRect(18,47,556,24),@"Import a GoSync YAML or JSON file");
    _configPath.accessibilityLabel = @"Configuration file";
    _reveal = GSButton(config,NSMakeRect(14,76,144,24),@"Show in Finder",@"config-folder",self,@selector(sendAction:));
    GSText(config,NSMakeRect(170,80,404,17),@"Imports preserve your identity and sync history.",11,NSFontWeightRegular,YES);
    _remove = GSButton(root,NSMakeRect(18,548,192,28),@"Remove Service…",@"remove-service",self,@selector(sendAction:));
    _feedback = GSText(root,NSMakeRect(226,553,390,20),@"Closing this window keeps GoSync running.",11,NSFontWeightRegular,YES);
    _feedback.alignment = NSTextAlignmentRight;
    return self;
}
- (void)update:(NSDictionary *)data {
    self.data = data;
    BOOL busy = [data[@"busy"] boolValue];
    BOOL selected = GSString(data,@"configPath").length > 0;
    BOOL canChange = [data[@"canChangeFolder"] boolValue] && !busy;
    GSSetText(_folderName, GSString(data,@"fullPath").length ? GSString(data,@"folder") : @"Choose your sync folder");
    GSSetPath(_folderPath, GSString(data,@"fullPath"));
    GSSetPath(_configPath, GSString(data,@"configPath"));
    GSSetText(_folderHint, !canChange && selected ? @"Stop synchronization before switching folders." : @"Previous folders, configurations, and sync history stay intact.");
    GSSetText(_health,GSString(data,@"title"));
    GSSetText(_security, !selected ? @"Connection mode appears after selecting a folder." : [GSString(data,@"security") isEqualToString:@"Mutual TLS"] ? @"Mutual TLS · authenticated and encrypted" : [GSString(data,@"security") isEqualToString:@"Open LAN"] ? @"Open LAN · unauthenticated and unencrypted" : @"Configuration needs attention before starting.");
    _chooseFolder.enabled = _chooseConfig.enabled = canChange;
    _chooseFolder.title = selected ? @"Choose New Folder…" : @"Choose Folder…";
    BOOL stop = [data[@"canStop"] boolValue];
    _startStop.title = stop ? @"Stop" : @"Start";
    _startStop.enabled = stop || [data[@"canStart"] boolValue];
    _login.state = [data[@"login"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
    _trayLogin.state = [data[@"trayLogin"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
    _login.enabled = [data[@"installed"] boolValue] && !busy;
    _trayLogin.enabled = !busy;
    _reveal.enabled = selected && !busy;
    _remove.enabled = ([data[@"installed"] boolValue] || [data[@"trayLogin"] boolValue]) && !busy;
    GSSetText(_feedback,busy ? @"Applying changes…" : @"Closing this window keeps GoSync running.");
}
- (void)sendAction:(NSButton *)sender { GSDispatchWindowAction(sender.identifier.UTF8String); }
- (void)startStop:(id)sender { GSDispatchWindowAction([self.data[@"canStop"] boolValue] ? "stop" : "start"); }
- (void)pickFolder:(id)sender {
    if (self.window.attachedSheet) return;
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    panel.title = @"Choose a synchronization folder";
    panel.prompt = @"Choose Folder";
    panel.canChooseDirectories = YES; panel.canChooseFiles = NO;
    panel.allowsMultipleSelection = NO; panel.canCreateDirectories = YES;
    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) {
        [panel orderOut:nil];
        if (result == NSModalResponseOK) GSDispatchWindowAction([@"choose-folder:" stringByAppendingString:panel.URL.path].UTF8String);
    }];
}
- (void)pickConfig:(id)sender {
    if (self.window.attachedSheet) return;
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    panel.title = @"Use an existing GoSync configuration";
    panel.prompt = @"Use Configuration";
    panel.canChooseDirectories = NO; panel.canChooseFiles = YES;
    panel.allowsMultipleSelection = NO;
    panel.allowedContentTypes = @[[UTType typeWithFilenameExtension:@"yaml"], [UTType typeWithFilenameExtension:@"yml"], UTTypeJSON];
    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) {
        [panel orderOut:nil];
        if (result == NSModalResponseOK) GSDispatchWindowAction([@"choose-config:" stringByAppendingString:panel.URL.path].UTF8String);
    }];
}
@end

@interface GSActivityController : NSWindowController <NSTableViewDataSource,NSTableViewDelegate>
@property NSDictionary *data;
@property NSArray<NSDictionary *> *rows;
@property NSTextField *title, *subtitle, *peerCount, *receive, *send, *chartCaption, *empty, *footnote, *freshness;
@property NSTextView *rowDetail;
@property GSActivityGraph *graph;
@property NSImageView *healthIcon;
@property NSSegmentedControl *tabs;
@property NSTableView *table;
@property NSScrollView *tableScroll;
@property NSButton *rescan;
- (void)update:(NSDictionary *)data;
@end

@implementation GSActivityController
- (instancetype)init {
    self = [super initWithWindow:GSWindow(@"GoSync Activity",NSMakeSize(840,690))];
    if (!self) return nil;
    NSView *root = self.window.contentView;
    _healthIcon = [[NSImageView alloc] initWithFrame:NSMakeRect(26,25,30,30)];
    [root addSubview:_healthIcon];
    _title = GSText(root,NSMakeRect(70,20,480,30),@"Activity",23,NSFontWeightSemibold,NO);
    _subtitle = GSText(root,NSMakeRect(70,55,590,18),@"Choose a folder in Settings to get started.",12,NSFontWeightRegular,YES);
    _rescan = GSButton(root,NSMakeRect(690,27,126,30),@"Rescan Now",@"rescan",self,@selector(rescan:));
    GSButton(root,NSMakeRect(568,27,110,30),@"Settings",@"settings",self,@selector(settings:));
    NSArray *names = @[@"RECENT PEERS",@"↓  RECEIVE",@"↑  SEND"];
    NSMutableArray *metrics = [NSMutableArray array];
    for (NSInteger i = 0; i < 3; i++) {
        NSView *card = GSAddCard(root,NSMakeRect(24+i*269,94,254,70));
        NSTextField *label = GSText(card,NSMakeRect(16,10,220,16),names[i],10,NSFontWeightMedium,YES);
        if (i == 1) label.textColor = NSColor.systemTealColor;
        if (i == 2) label.textColor = NSColor.systemPurpleColor;
        NSTextField *value = GSText(card,NSMakeRect(16,32,220,28),@"—",22,NSFontWeightMedium,NO);
        value.font = [NSFont monospacedDigitSystemFontOfSize:22 weight:NSFontWeightMedium];
        value.accessibilityLabel = names[i];
        [metrics addObject:value];
    }
    _peerCount = metrics[0]; _receive = metrics[1]; _send = metrics[2];
    NSView *chart = GSAddCard(root,NSMakeRect(24,178,792,120));
    GSText(chart,NSMakeRect(16,10,300,18),@"Transfer activity · last 60 seconds",11,NSFontWeightMedium,YES);
    _graph = [[GSActivityGraph alloc] initWithFrame:NSMakeRect(16,38,760,54)];
    [chart addSubview:_graph];
    _chartCaption = GSText(chart,NSMakeRect(16,98,760,16),@"Waiting for measurements",10,NSFontWeightRegular,YES);
    _tabs = [NSSegmentedControl segmentedControlWithLabels:@[@"Peers",@"Transfers",@"Attention"] trackingMode:NSSegmentSwitchTrackingSelectOne target:self action:@selector(changeTab:)];
    _tabs.frame = NSMakeRect(24,316,340,28); _tabs.selectedSegment = 0;
    [root addSubview:_tabs];
    _freshness = GSText(root,NSMakeRect(376,322,440,18),@"No live snapshot",11,NSFontWeightRegular,YES);
    _freshness.alignment = NSTextAlignmentRight;
    _table = [[NSTableView alloc] initWithFrame:NSMakeRect(0,0,790,218)];
    _table.delegate = self; _table.dataSource = self;
    _table.rowHeight = 35;
    _table.style = NSTableViewStyleFullWidth;
    _table.usesAlternatingRowBackgroundColors = YES;
    _table.allowsMultipleSelection = NO;
    _table.columnAutoresizingStyle = NSTableViewLastColumnOnlyAutoresizingStyle;
    _tableScroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(24,356,792,218)];
    _tableScroll.hasVerticalScroller = YES;
    _tableScroll.borderType = NSBezelBorder;
    _tableScroll.documentView = _table;
    [root addSubview:_tableScroll];
    _empty = GSText(root,NSMakeRect(48,436,744,50),@"No peers yet",14,NSFontWeightRegular,YES);
    _empty.alignment = NSTextAlignmentCenter;
    _empty.maximumNumberOfLines = 2; _empty.lineBreakMode = NSLineBreakByWordWrapping;
    [root addSubview:_empty];
    NSScrollView *detailScroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(24,586,792,54)];
    detailScroll.hasVerticalScroller = YES;
    detailScroll.drawsBackground = NO;
    _rowDetail = [[NSTextView alloc] initWithFrame:NSMakeRect(0,0,792,54)];
    _rowDetail.editable = NO; _rowDetail.selectable = YES;
    _rowDetail.drawsBackground = NO;
    _rowDetail.font = [NSFont systemFontOfSize:11];
    _rowDetail.textColor = NSColor.secondaryLabelColor;
    _rowDetail.textContainerInset = NSMakeSize(0,2);
    _rowDetail.textContainer.widthTracksTextView = YES;
    _rowDetail.autoresizingMask = NSViewWidthSizable;
    _rowDetail.accessibilityLabel = @"Selected row details";
    detailScroll.documentView = _rowDetail;
    [root addSubview:detailScroll];
    _footnote = GSText(root,NSMakeRect(24,653,792,19),@"Locally idle does not mean every peer has finished syncing.",11,NSFontWeightRegular,YES);
    [self configureTable];
    return self;
}
- (void)rescan:(id)sender { GSDispatchWindowAction("rescan"); }
- (void)settings:(id)sender { GSDispatchWindowAction("settings"); }
- (void)configureTable {
    for (NSTableColumn *column in [_table.tableColumns copy]) [_table removeTableColumn:column];
    NSArray *keys, *titles, *widths;
    switch (_tabs.selectedSegment) {
        case 1: keys=@[@"name",@"received",@"target",@"time"]; titles=@[@"Receiving file",@"Payload received",@"Target size",@"Started"]; widths=@[@310,@155,@125,@180]; break;
        case 2: keys=@[@"name",@"detail"]; titles=@[@"Item",@"Needs attention"]; widths=@[@260,@510]; break;
        default: keys=@[@"name",@"state",@"time"]; titles=@[@"Peer",@"Status",@"Last successful sync"]; widths=@[@330,@220,@220]; break;
    }
    for (NSUInteger i=0;i<keys.count;i++) {
        NSTableColumn *column = [[NSTableColumn alloc] initWithIdentifier:keys[i]];
        column.title = titles[i]; column.width = [widths[i] doubleValue]; column.minWidth = 90;
        [_table addTableColumn:column];
    }
}
- (void)changeTab:(id)sender {
    [_table deselectAll:nil];
    [self configureTable];
    [self update:self.data ?: @{}];
}
- (NSInteger)numberOfRowsInTableView:(NSTableView *)tableView { return self.rows.count; }
- (NSView *)tableView:(NSTableView *)tableView viewForTableColumn:(NSTableColumn *)column row:(NSInteger)row {
    NSTextField *cell = [tableView makeViewWithIdentifier:column.identifier owner:self];
    if (!cell) {
        cell = [NSTextField labelWithString:@""];
        cell.identifier = column.identifier;
        cell.font = [NSFont systemFontOfSize:12];
        cell.lineBreakMode = [column.identifier isEqualToString:@"name"] ? NSLineBreakByTruncatingMiddle : NSLineBreakByTruncatingTail;
    }
    cell.stringValue = self.rows[row][column.identifier] ?: @"";
    cell.toolTip = cell.stringValue;
    cell.accessibilityLabel = [NSString stringWithFormat:@"%@: %@",column.title,cell.stringValue];
    return cell;
}
- (void)tableViewSelectionDidChange:(NSNotification *)notification { [self updateRowDetail]; }
- (void)updateRowDetail {
    NSInteger row = _table.selectedRow;
    NSString *detail;
    if (row >= 0 && row < self.rows.count) detail = self.rows[row][@"detail"];
    else if (_tabs.selectedSegment == 1) detail = @"Payload counts only bytes received in this attempt, not reused blocks. It is not a completion percentage. Uploads are counted, but per-file upload details are not reported by the service.";
    else if (_tabs.selectedSegment == 2) detail = self.rows.count ? @"Select an item to read its full explanation." : @"Conflicts, scan failures, unsupported paths, and peer errors appear here.";
    else detail = @"Peers are recently reachable after a successful reconciliation; this is not a live connection count. Select a peer for its identity and any reported error.";
    _rowDetail.string = detail ?: @"";
}
- (void)update:(NSDictionary *)data {
    self.data = data;
    GSSetText(_title,GSString(data,@"title"));
    GSSetText(_subtitle,GSString(data,@"fullPath").length ? GSString(data,@"fullPath") : @"Choose a folder in Settings to get started.");
    GSSetText(_peerCount,GSString(data,@"peers")); GSSetText(_receive,GSString(data,@"receive")); GSSetText(_send,GSString(data,@"send"));
    _rescan.enabled = [data[@"canRescan"] boolValue];
    NSString *health = GSString(data,@"health");
    NSDictionary *symbols = @{@"idle":@"checkmark.circle.fill",@"syncing":@"arrow.triangle.2.circlepath",@"warning":@"exclamationmark.triangle.fill",@"error":@"xmark.circle.fill",@"stopped":@"stop.circle"};
    _healthIcon.image = [NSImage imageWithSystemSymbolName:symbols[health] ?: @"circle.dotted" accessibilityDescription:GSString(data,@"title")];
    _healthIcon.contentTintColor = [health isEqualToString:@"error"] ? NSColor.systemRedColor : [health isEqualToString:@"warning"] ? NSColor.systemOrangeColor : [health isEqualToString:@"idle"] || [health isEqualToString:@"syncing"] ? NSColor.systemTealColor : NSColor.secondaryLabelColor;
    BOOL fresh = [data[@"fresh"] boolValue];
    NSString *heartbeat = GSString(data,@"heartbeat");
    BOOL historical = heartbeat.length && ![heartbeat isEqualToString:@"Not yet"];
    GSSetText(_freshness, fresh ? [@"Live · " stringByAppendingString:heartbeat] : historical ? [@"Historical · " stringByAppendingString:heartbeat] : @"No live snapshot");
    _graph.points = [data[@"points"] isKindOfClass:NSArray.class] ? data[@"points"] : @[];
    _graph.now = [data[@"now"] doubleValue]; _graph.fresh = fresh; _graph.peak = 0;
    BOOL measured = NO;
    for (NSDictionary *point in _graph.points) if ([point[@"valid"] boolValue]) { measured = YES; _graph.peak = MAX(_graph.peak,MAX([point[@"receive"] doubleValue],[point[@"send"] doubleValue])); }
    _graph.needsDisplay = YES;
    NSString *peak = _graph.peak >= 1e6 ? [NSString stringWithFormat:@"%.1f MB/s",_graph.peak/1e6] : _graph.peak >= 1e3 ? [NSString stringWithFormat:@"%.1f kB/s",_graph.peak/1e3] : [NSString stringWithFormat:@"%.0f B/s",_graph.peak];
    GSSetText(_chartCaption,!measured ? @"Waiting for measurements" : [NSString stringWithFormat:@"Receive: solid teal · Send: dashed purple                         Peak %@%@",peak,fresh ? @"" : @" · historical"]);
    NSString *key = _tabs.selectedSegment == 1 ? @"transferRows" : _tabs.selectedSegment == 2 ? @"issueRows" : @"peerRows";
    NSArray *next = [data[key] isKindOfClass:NSArray.class] ? data[key] : @[];
    if (![self.rows isEqual:next]) {
        NSString *selected = _table.selectedRow >= 0 && _table.selectedRow < self.rows.count ? self.rows[_table.selectedRow][@"name"] : nil;
        self.rows = next;
        [_table reloadData];
        [_table deselectAll:nil];
        for (NSUInteger i=0;i<next.count;i++) if ([next[i][@"name"] isEqual:selected]) { [_table selectRowIndexes:[NSIndexSet indexSetWithIndex:i] byExtendingSelection:NO]; break; }
    }
    _empty.hidden = self.rows.count > 0;
    NSString *empty = _tabs.selectedSegment == 1 ? @"No receiving files in this snapshot" : _tabs.selectedSegment == 2 ? @"No issues reported in this snapshot" : @"No peers discovered yet";
    if (!GSString(data,@"configPath").length) empty = @"Choose a folder in Settings to see activity here.";
    else if (!fresh && !historical) empty = @"Start synchronization to see live activity.";
    GSSetText(_empty,empty);
    GSSetText(_footnote, _tabs.selectedSegment == 1 && fresh ? [NSString stringWithFormat:@"%@ receiving · %@ sending · Upload file names are not included in the snapshot.",data[@"downloads"] ?: @0,data[@"uploads"] ?: @0] : !fresh && historical ? @"These are last-known values. Start synchronization to receive fresh measurements." : @"Locally idle does not mean every peer has finished syncing.");
    [self updateRowDetail];
}
@end

static GSSettingsController *settingsController;
static GSActivityController *activityController;
static NSDictionary *windowSnapshot;

void GSShowWindow(const char *name) {
    NSString *kind = [NSString stringWithUTF8String:name];
    dispatch_async(dispatch_get_main_queue(), ^{
        if ([kind isEqualToString:@"settings"]) {
            if (!settingsController) settingsController = [[GSSettingsController alloc] init];
            [settingsController update:windowSnapshot ?: @{}];
            GSFront(settingsController.window);
        } else {
            if (!activityController) activityController = [[GSActivityController alloc] init];
            [activityController update:windowSnapshot ?: @{}];
            GSFront(activityController.window);
        }
    });
}

void GSUpdateWindows(const char *json) {
    NSData *bytes = [[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    NSDictionary *data = [NSJSONSerialization JSONObjectWithData:bytes options:0 error:nil];
    if (![data isKindOfClass:NSDictionary.class]) return;
    CFRunLoopPerformBlock(CFRunLoopGetMain(), kCFRunLoopCommonModes, ^{
        windowSnapshot = data;
        [settingsController update:data];
        [activityController update:data];
    });
    CFRunLoopWakeUp(CFRunLoopGetMain());
}

int GSConfirm(const char *title, const char *message) {
    NSString *heading = [NSString stringWithUTF8String:title], *body = [NSString stringWithUTF8String:message];
    __block BOOL approved = NO;
    dispatch_sync(dispatch_get_main_queue(), ^{
        [NSApp activateIgnoringOtherApps:YES];
        NSAlert *alert = [[NSAlert alloc] init];
        alert.messageText = heading; alert.informativeText = body;
        alert.alertStyle = NSAlertStyleWarning;
        [alert addButtonWithTitle:@"Continue"].keyEquivalent = @"";
        [alert addButtonWithTitle:@"Cancel"].keyEquivalent = @"\r";
        approved = [alert runModal] == NSAlertFirstButtonReturn;
    });
    return approved ? 1 : 0;
}

void GSShowMessage(const char *title, const char *message, int failure) {
    NSString *heading = [NSString stringWithUTF8String:title], *body = [NSString stringWithUTF8String:message];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSAlert *alert = [[NSAlert alloc] init];
        alert.messageText = heading; alert.informativeText = body;
        alert.alertStyle = failure ? NSAlertStyleWarning : NSAlertStyleInformational;
        [alert addButtonWithTitle:@"OK"];
        [NSApp activateIgnoringOtherApps:YES];
        NSWindow *parent = NSApp.keyWindow;
        if (parent && parent != alert.window) [alert beginSheetModalForWindow:parent completionHandler:nil];
        else [alert runModal];
    });
}
