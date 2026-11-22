#import <Cocoa/Cocoa.h>

// Shared by the menu and Activity window; all calls belong on AppKit's main loop.
@interface GSActivityGraph : NSView
@property(copy) NSArray<NSDictionary *> *points;
@property double now;
@property double peak;
@property BOOL fresh;
@end
