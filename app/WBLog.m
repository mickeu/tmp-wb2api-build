// WBLog.m — 日志落盘与读取（见 WBLog.h）。
#import "WBLog.h"
#import <fcntl.h>
#import <sys/stat.h>
#import <unistd.h>
#import "libwbgateway.h"

static const NSUInteger kMaxLogBytes = 2 * 1024 * 1024; // 2MB 轮转

static dispatch_queue_t WBLogQueue(void) {
    static dispatch_queue_t q;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        q = dispatch_queue_create("com.mickeu.wbgateway.log", DISPATCH_QUEUE_SERIAL);
    });
    return q;
}

// 当前 fd（只在 WBLogQueue 上读写）。
static int s_fd = -1;

@implementation WBLog

+ (NSString *)logPath {
    NSArray<NSString *> *dirs = NSSearchPathForDirectoriesInDomains(NSCachesDirectory, NSUserDomainMask, YES);
    return [dirs.firstObject stringByAppendingPathComponent:@"wbgw.log"];
}

+ (NSString *)rotatedPath {
    return [[self logPath] stringByAppendingString:@".1"];
}

// 在队列上打开 fd（已打开则直接返回）。
+ (int)openFDLocked {
    if (s_fd >= 0) return s_fd;
    NSString *path = [self logPath];
    s_fd = open(path.fileSystemRepresentation, O_WRONLY | O_CREAT | O_APPEND, 0644);
    return s_fd;
}

// 轮转（队列上调用）：旧文件 → wbgw.log.1，新建空文件，新 fd 注入 Go。
// 顺序：先改名旧文件，再开新文件并注入，最后（延迟 1 秒）关旧 fd——
// Go 侧 in-flight 的写仍可能落到旧 fd（已改名成 .1），不丢；
// 1 秒后关 fd，Go 的 wrapper 早已指向新 fd。
+ (void)rotateLocked {
    NSString *path = [self logPath];
    NSString *rotated = [self rotatedPath];
    NSFileManager *fm = [NSFileManager defaultManager];
    int oldFD = s_fd;
    // 旧文件改名（fd 仍指向它，路径已变）。
    [fm removeItemAtPath:rotated error:nil];
    [fm moveItemAtPath:path toPath:rotated error:nil];
    // 开新文件。
    s_fd = -1;
    int newFD = [self openFDLocked];
    if (newFD >= 0) {
        WBG_SetLogFD(newFD);
        [self logLineLocked:@"--- 日志轮转：旧日志已移到 wbgw.log.1 ---"];
    }
    if (oldFD >= 0) {
        // 延迟关旧 fd，让 Go 侧 in-flight 的写先落盘。
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1 * NSEC_PER_SEC)),
                       WBLogQueue(), ^{
            close(oldFD);
        });
    }
}

// 写一行（队列上调用，fd 已打开）。
+ (void)logLineLocked:(NSString *)line {
    int fd = [self openFDLocked];
    if (fd < 0) return;
    // 轮转检查：写之前看大小。
    struct stat st;
    if (fstat(fd, &st) == 0 && (NSUInteger)st.st_size > kMaxLogBytes) {
        [self rotateLocked];
        fd = s_fd;
        if (fd < 0) return;
    }
    static NSDateFormatter *fmt = nil;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        fmt = [[NSDateFormatter alloc] init];
        fmt.dateFormat = @"yyyy-MM-dd HH:mm:ss";
        fmt.locale = [[NSLocale alloc] initWithLocaleIdentifier:@"en_US_POSIX"];
    });
    NSString *full = [NSString stringWithFormat:@"[%@] %@\n", [fmt stringFromDate:[NSDate date]], line];
    const char *bytes = full.UTF8String;
    size_t len = strlen(bytes);
    size_t off = 0;
    while (off < len) {
        ssize_t n = write(fd, bytes + off, len - off);
        if (n <= 0) break;
        off += (size_t)n;
    }
}

+ (void)logMessage:(NSString *)message {
    if (!message) return;
    dispatch_async(WBLogQueue(), ^{
        [self logLineLocked:message];
    });
}

+ (void)logFormat:(NSString *)format, ... {
    va_list args;
    va_start(args, format);
    NSString *msg = [[NSString alloc] initWithFormat:format arguments:args];
    va_end(args);
    [self logMessage:msg];
}

+ (int)logFD {
    __block int fd = -1;
    dispatch_sync(WBLogQueue(), ^{
        fd = [self openFDLocked];
    });
    return fd;
}

+ (NSArray<NSString *> *)tailLines:(NSUInteger)count {
    __block NSArray<NSString *> *result = @[];
    dispatch_sync(WBLogQueue(), ^{
        NSString *path = [self logPath];
        NSData *data = [NSData dataWithContentsOfFile:path];
        if (!data.length) return;
        NSString *text = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
        if (!text) return;
        NSArray<NSString *> *lines = [text componentsSeparatedByString:@"\n"];
        NSMutableArray<NSString *> *nonEmpty = [NSMutableArray array];
        for (NSString *l in lines) {
            if (l.length) [nonEmpty addObject:l];
        }
        NSUInteger n = MIN(count, nonEmpty.count);
        if (n) result = [nonEmpty subarrayWithRange:NSMakeRange(nonEmpty.count - n, n)];
    });
    return result;
}

@end

int wb_log_fd(void) {
    return [WBLog logFD];
}

NSArray<NSString *> *wb_log_tail(NSUInteger count) {
    return [WBLog tailLines:count];
}

void wb_log_msg(const char *msg) {
    if (!msg) return;
    [WBLog logMessage:[NSString stringWithUTF8String:msg]];
}
