// Exercise the production AppKit views without starting a daemon or changing
// desktop appearance. These fixture checks are not live-transfer evidence.
#import <Cocoa/Cocoa.h>
#import "../internal/desktop/ui/windows_darwin.m"

static NSMutableArray<NSString *> *actions;
void GSDispatchWindowAction(const char *action) { [actions addObject:[NSString stringWithUTF8String:action]]; }
static void check(BOOL value, NSString *message) {
    if (!value) { fprintf(stderr,"FAIL: %s\n",message.UTF8String); exit(1); }
}
int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        actions = [NSMutableArray array];
        GSSettingsController *settings = [[GSSettingsController alloc] init];
        GSActivityController *activity = [[GSActivityController alloc] init];
        // Go's empty history serializes to JSON null before the first sample.
        NSMutableDictionary *data = [@{@"title":@"Not configured",@"canChangeFolder":@YES,@"heartbeat":@"Not yet",@"points":NSNull.null} mutableCopy];
        [settings update:data]; [activity update:data];
        check(settings.chooseFolder.enabled && settings.chooseConfig.enabled,@"empty setup exposes pickers");
        check(!settings.startStop.enabled && !settings.login.enabled,@"unconfigured service actions disabled");
        check(![settings.security.stringValue containsString:@"Mutual TLS"],@"no invented security mode");
        check(!activity.empty.hidden && activity.table.numberOfRows == 0,@"empty activity explains setup");
        [data addEntriesFromDictionary:@{@"configPath":@"/private/profile.json",@"fullPath":@"/private/Workspace",@"folder":@"Workspace",@"security":@"Mutual TLS",@"title":@"Syncing",@"health":@"syncing",@"canChangeFolder":@NO,@"canStop":@YES,@"installed":@YES,@"fresh":@YES,@"login":@YES,@"canRescan":@YES,@"heartbeat":@"Nov 22, 14:00:00",@"peers":@"1 / 1",@"receive":@"1.2 MB/s",@"send":@"800 kB/s",@"peerRows":@[@{@"name":@"127.0.0.1:1234",@"state":@"Recently reachable",@"time":@"Just now",@"detail":@"Node: peer-1"}],@"transferRows":@[@{@"name":@"a<b>.bin",@"received":@"1.0 MiB",@"target":@"8.0 MiB",@"time":@"Just now",@"detail":@"Payload received this attempt, not a completion percentage."}]}];
        [settings update:data]; [activity update:data];
        check(!settings.chooseFolder.enabled && !settings.chooseConfig.enabled,@"running folder cannot switch");
        check(settings.startStop.enabled && [settings.startStop.title isEqualToString:@"Stop"],@"running service exposes stop");
        check(actions.count == 0,@"snapshot updates must not invoke login or service actions");
        [settings startStop:nil];
        check([actions.lastObject isEqualToString:@"stop"],@"stop routes through existing controller");
        check(activity.table.numberOfRows == 1 && activity.empty.hidden,@"live peer rows are populated");
        [activity.table selectRowIndexes:[NSIndexSet indexSetWithIndex:0] byExtendingSelection:NO];
        [activity updateRowDetail];
        check([activity.rowDetail.string isEqualToString:@"Node: peer-1"],@"selected peer details readable");
        activity.tabs.selectedSegment = 1; [activity changeTab:nil];
        check(activity.table.numberOfRows == 1 && [activity.table.tableColumns[0].title isEqualToString:@"Receiving file"],@"transfers tab has structured rows");
        check([activity.rowDetail.string containsString:@"not a completion percentage"],@"no misleading progress percentage");
        data[@"fresh"] = @NO; data[@"canStop"] = @NO; data[@"canStart"] = @YES; data[@"canRescan"] = @NO; data[@"canChangeFolder"] = @YES;
        [activity update:data]; [settings update:data];
        check([activity.freshness.stringValue hasPrefix:@"Historical"],@"stale snapshot explicitly historical");
        check(!activity.rescan.enabled && settings.chooseFolder.enabled,@"stopped state gating");
        for (NSAppearanceName name in @[NSAppearanceNameAqua,NSAppearanceNameDarkAqua]) {
            for (NSWindow *window in @[settings.window,activity.window]) {
                window.appearance = [NSAppearance appearanceNamed:name];
                [window.contentView layoutSubtreeIfNeeded];
                NSBitmapImageRep *bitmap = [window.contentView bitmapImageRepForCachingDisplayInRect:window.contentView.bounds];
                [window.contentView cacheDisplayInRect:window.contentView.bounds toBitmapImageRep:bitmap];
                check(bitmap.pixelsWide > 0 && bitmap.pixelsHigh > 0,@"native window renders in both appearances");
                check([[window.effectiveAppearance bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua,NSAppearanceNameDarkAqua]] isEqualToString:name],@"window follows requested appearance");
            }
        }
        [settings.window close]; [activity.window close];
        check(actions.count == 1,@"closing windows never stops the daemon");
        puts("AppKit window checks passed: empty, live, historical, actions, tables, light/dark rendering, window closure.");
    }
    return 0;
}
