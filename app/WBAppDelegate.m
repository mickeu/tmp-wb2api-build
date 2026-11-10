// WBAppDelegate.m — 启动顺序：开日志交 fd → 设 rootViewController →
// 音频 delegate → 立即启动网关 → 刷新状态。
// 后台：申请后台任务 + 启动静音音频保活；回前台刷新状态；退出时停网关。
#import "WBAppDelegate.h"
#import "WBLog.h"
#import "WBServer.h"
#import "WBAudioKeepAlive.h"
#import "WBStatusViewController.h"

@interface WBAppDelegate ()
@property (nonatomic, assign) UIBackgroundTaskIdentifier bgTask;
@end

@implementation WBAppDelegate

- (BOOL)application:(UIApplication *)application didFinishLaunchingWithOptions:(NSDictionary *)launchOptions {
    self.bgTask = UIBackgroundTaskInvalid;
    // 1. 开日志（fd 交给 Go；WBLog 内部 lazy open，这里先暖一下）。
    [WBLog logMessage:@"WB2Gateway 启动"];
    // 2. 根视图。
    self.window = [[UIWindow alloc] initWithFrame:[UIScreen mainScreen].bounds];
    WBStatusViewController *vc = [[WBStatusViewController alloc] init];
    self.window.rootViewController = vc;
    [self.window makeKeyAndVisible];
    // 3. 音频保活（delegate 由 WBAudioKeepAlive 内部注册通知）。
    [[WBAudioKeepAlive sharedKeepAlive] start];
    // 4. 立即启动网关（配置缺失时从 bundle 复制示例配置）。
    // auto_start=false 时跳过自启（显式启动仍可用按钮 / URL scheme）。
    BOOL ok = YES;
    if ([[WBServer sharedServer] autoStart]) {
        ok = [[WBServer sharedServer] start];
        if (!ok) {
            [WBLog logFormat:@"启动时网关未起来：%@", [[WBServer sharedServer] lastError] ?: @"未知错误"];
        }
    } else {
        [WBLog logMessage:@"auto_start=false：冷启动跳过网关自启"];
    }
    // 5. 刷新状态（VC 在 viewDidAppear 里也会刷新一次）。
    [vc refreshStatus];
    return YES;
}

- (void)applicationDidEnterBackground:(UIApplication *)application {
    // 后台任务 + 静音音频保活，让网关进程不被挂起。
    if (self.bgTask == UIBackgroundTaskInvalid) {
        __weak typeof(self) weakSelf = self;
        self.bgTask = [application beginBackgroundTaskWithName:@"WB2Gateway" expirationHandler:^{
            __strong typeof(weakSelf) strongSelf = weakSelf;
            if (!strongSelf) return;
            [application endBackgroundTask:strongSelf.bgTask];
            strongSelf.bgTask = UIBackgroundTaskInvalid;
        }];
    }
    [[WBAudioKeepAlive sharedKeepAlive] start];
    [WBLog logMessage:@"进入后台：保活已确保启动"];
}

- (void)applicationWillEnterForeground:(UIApplication *)application {
    if (self.bgTask != UIBackgroundTaskInvalid) {
        [application endBackgroundTask:self.bgTask];
        self.bgTask = UIBackgroundTaskInvalid;
    }
    // 回前台：若网关没在跑（冷启动自启失败等），幂等补启一次（auto_start=false 时不补启）。
    if ([[WBServer sharedServer] autoStart] && ![[WBServer sharedServer] isRunning]) {
        [WBLog logMessage:@"回前台：网关未运行，尝试补启"];
        if (![[WBServer sharedServer] start]) {
            [WBLog logFormat:@"回前台补启网关失败：%@", [[WBServer sharedServer] lastError] ?: @"未知错误"];
        }
    }
    // 回前台刷新状态（VC 也监听了该通知，双保险）。
    [[NSNotificationCenter defaultCenter] postNotificationName:@"WB2GatewayRefreshStatus" object:nil];
}

- (void)applicationWillTerminate:(UIApplication *)application {
    [[WBServer sharedServer] stop];
    [[WBAudioKeepAlive sharedKeepAlive] stop];
}

// URL scheme：wbgateway://start | wbgateway://stop | wbgateway://status
- (BOOL)application:(UIApplication *)app openURL:(NSURL *)url options:(NSDictionary<UIApplicationOpenURLOptionsKey, id> *)options {
    if (![[url scheme] isEqualToString:@"wbgateway"]) return NO;
    NSString *action = url.host.lowercaseString;
    if ([action isEqualToString:@"start"]) {
        [[WBServer sharedServer] start];
        [[WBAudioKeepAlive sharedKeepAlive] start];
    } else if ([action isEqualToString:@"stop"]) {
        [[WBServer sharedServer] stop];
        [[WBAudioKeepAlive sharedKeepAlive] stop];
    } else if ([action isEqualToString:@"status"]) {
        // 状态已在界面上；这里只刷新。
    } else {
        return NO;
    }
    [[NSNotificationCenter defaultCenter] postNotificationName:@"WB2GatewayRefreshStatus" object:nil];
    return YES;
}

@end
