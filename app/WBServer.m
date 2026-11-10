// WBServer.m — Go 网关桥接（见 WBServer.h）。
//
// 注意 WBG_LastError 的所有权：返回的 C 字符串归 Go 侧所有，调用方只读并
// 拷走，**不要 free**，否则 double-free（见 go/ioslib.go 的注释）。
#import "WBServer.h"
#import "WBLog.h"
#import "libwbgateway.h"
#include <stdlib.h>
#include <time.h>

const NSInteger WBServerDefaultPort = 17865;

// 面板请求超时（秒）。
static const NSTimeInterval kPanelTimeout = 1.5;

@implementation WBServer

+ (instancetype)sharedServer {
    static WBServer *s;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ s = [[WBServer alloc] init]; });
    return s;
}

+ (NSString *)configDir {
    NSArray<NSString *> *dirs = NSSearchPathForDirectoriesInDomains(NSApplicationSupportDirectory, NSUserDomainMask, YES);
    return [dirs.firstObject stringByAppendingPathComponent:@"wb2api"];
}

+ (NSString *)configPath {
    return [[self configDir] stringByAppendingPathComponent:@"config.json"];
}

// 准备配置目录与文件：建目录、排除 iCloud 备份、首次从 bundle 复制示例配置。
+ (BOOL)prepareConfig:(NSError **)outErr {
    NSFileManager *fm = [NSFileManager defaultManager];
    NSString *dir = [self configDir];
    if (![fm fileExistsAtPath:dir]) {
        if (![fm createDirectoryAtPath:dir withIntermediateDirectories:YES attributes:nil error:outErr]) {
            return NO;
        }
    }
    NSURL *dirURL = [NSURL fileURLWithPath:dir];
    // 状态与配置不进 iCloud 备份。
    [dirURL setResourceValue:@YES forKey:NSURLIsExcludedFromBackupKey error:nil];
    NSString *cfg = [self configPath];
    if (![fm fileExistsAtPath:cfg]) {
        NSString *example = [[NSBundle mainBundle] pathForResource:@"config.example" ofType:@"json"];
        if (example && [fm fileExistsAtPath:example]) {
            if (![fm copyItemAtPath:example toPath:cfg error:outErr]) return NO;
            [WBLog logMessage:@"首次启动：已从 bundle 复制 config.example.json"];
        } else {
            if (outErr) {
                *outErr = [NSError errorWithDomain:@"WBServer" code:1
                                          userInfo:@{NSLocalizedDescriptionKey: @"bundle 里没有 config.example.json"}];
            }
            return NO;
        }
    }
    return YES;
}

// 必须在 WBG_Start 之前：沙盒读不到 /var/db/timezone，缺 TZ 时 Go 回退 UTC（实测日志差 8h）。
// tzset() 让后续 setenv 对 C 库生效，Go 侧另有一次显式设置兜底（见 go/ioslib.go applyTimeZone）。
static void WBApplyLocalTimeZone(void) {
    NSString *name = NSTimeZone.localTimeZone.name;
    if (name.length > 0) {
        setenv("TZ", name.UTF8String, 1);
        tzset();
    }
}

- (BOOL)start {
    if ([self isRunning]) return YES;
    NSError *err = nil;
    if (![[self class] prepareConfig:&err]) {
        [WBLog logFormat:@"启动失败：准备配置出错：%@", err.localizedDescription];
        return NO;
    }
    NSString *cfg = [[self class] configPath];
    // 先把日志 fd 交给 Go（读配置、DNS 等早期日志也落盘）。
    int fd = wb_log_fd();
    if (fd >= 0) WBG_SetLogFD(fd);
    WBApplyLocalTimeZone();        // ← 必须在 WBG_Start 之前
    int rc = WBG_Start((char *)cfg.UTF8String, (int)WBServerDefaultPort);
    if (rc != 0) {
        [WBLog logFormat:@"启动失败：%@", [self lastError] ?: @"未知错误"];
        return NO;
    }
    [WBLog logFormat:@"网关已启动：%@（端口 %ld）", cfg, (long)[self port]];
    return YES;
}

- (void)stop {
    WBG_Stop();
    [WBLog logMessage:@"网关已停止"];
}

- (BOOL)isRunning {
    return WBG_Port() != 0;
}

- (NSInteger)port {
    return (NSInteger)WBG_Port();
}

- (nullable NSString *)lastError {
    const char *c = WBG_LastError();
    if (!c) return nil;
    // 只读并拷走，不 free（字符串归 Go 侧所有）。
    return [NSString stringWithUTF8String:c];
}

- (NSString *)baseURLString {
    NSInteger p = [self port];
    if (p == 0) p = WBServerDefaultPort;
    return [NSString stringWithFormat:@"http://127.0.0.1:%ld/v1", (long)p];
}

- (NSString *)apiKey {
    NSData *data = [NSData dataWithContentsOfFile:[[self class] configPath]];
    if (!data) return @"";
    id json = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (![json isKindOfClass:[NSDictionary class]]) return @"";
    NSString *key = json[@"api_key"];
    return [key isKindOfClass:[NSString class]] ? key : @"";
}

// 冷启动是否自动启动网关：config.json 的 auto_start；键不存在、不是布尔值、
// 配置读不到时都默认 YES（与 Go 侧 Config.AutoStartEnabled 同语义）。
- (BOOL)autoStart {
    NSData *data = [NSData dataWithContentsOfFile:[[self class] configPath]];
    if (!data) return YES;
    id json = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (![json isKindOfClass:[NSDictionary class]]) return YES;
    id v = json[@"auto_start"];
    if (![v isKindOfClass:[NSNumber class]]) return YES;
    return [v boolValue];
}

- (NSDictionary *)statusSnapshot {
    return @{
        @"running": @([self isRunning]),
        @"port": @([self port]),
        @"address": [self baseURLString],
        @"error": [self lastError] ?: [NSNull null],
    };
}

// 同步 GET（只在后台线程调用）：1.5 秒超时，返回解析后的 JSON 或 nil。
// 面板接口要 Bearer 鉴权（api_key，见 panel.withAuth）。
- (nullable id)syncGET:(NSString *)path {
    NSString *urlString = [NSString stringWithFormat:@"http://127.0.0.1:%ld%@", (long)[self port], path];
    NSURL *url = [NSURL URLWithString:urlString];
    if (!url) return nil;
    NSMutableURLRequest *req = [NSMutableURLRequest requestWithURL:url];
    NSString *key = [self apiKey];
    if (key.length) {
        [req setValue:[NSString stringWithFormat:@"Bearer %@", key] forHTTPHeaderField:@"Authorization"];
    }
    NSURLSessionConfiguration *conf = [NSURLSessionConfiguration ephemeralSessionConfiguration];
    conf.timeoutIntervalForRequest = kPanelTimeout;
    conf.timeoutIntervalForResource = kPanelTimeout;
    NSURLSession *session = [NSURLSession sessionWithConfiguration:conf];
    __block NSData *body = nil;
    __block NSError *taskErr = nil;
    dispatch_semaphore_t sem = dispatch_semaphore_create(0);
    [[session dataTaskWithRequest:req completionHandler:^(NSData *data, NSURLResponse *response, NSError *error) {
        body = data;
        taskErr = error;
        dispatch_semaphore_signal(sem);
    }] resume];
    dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW, (int64_t)(kPanelTimeout * NSEC_PER_SEC));
    if (dispatch_semaphore_wait(sem, deadline) != 0) {
        return nil; // 超时
    }
    if (taskErr || !body) return nil;
    return [NSJSONSerialization JSONObjectWithData:body options:0 error:nil];
}

- (void)fetchPanelStatusWithCompletion:(void (^)(NSDictionary *result))completion {
    // 主线程之外执行网络请求，结果回主线程。
    // /panel/api/overview 没有独立的 /panel/api/pool 路由，总览里含池计数与每账号状态。
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        id credit = [self syncGET:@"/panel/api/credit_today"];
        id overview = [self syncGET:@"/panel/api/overview"];
        NSMutableDictionary *result = [NSMutableDictionary dictionary];
        if (credit) result[@"credit_today"] = credit;
        if (overview) result[@"overview"] = overview;
        if (!credit && !overview) result[@"error"] = @"面板接口无响应（网关未启动或超时）";
        dispatch_async(dispatch_get_main_queue(), ^{
            completion([result copy]);
        });
    });
}

@end
