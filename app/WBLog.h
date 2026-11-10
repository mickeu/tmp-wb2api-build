// WBLog.h — 日志落盘与读取。
//
// 日志文件：Library/Caches/wbgw.log（O_APPEND 打开，Go 侧与 ObjC 侧共用一个 fd，
// 写操作天然按追加顺序排队，不互相覆盖）。
// 轮转：文件超过 2MB 时把旧文件改名成 wbgw.log.1（只留 1 份），新建空文件，
// 并通过 WBG_SetLogFD 把新 fd 重新注入 Go（fdLogWriter 目标热换，Go 日志不丢）。
// 线程安全：所有文件操作走一条串行队列。
#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

@interface WBLog : NSObject

/// 写一条带时间戳的日志（文件 + 串行队列，线程安全）。
+ (void)logMessage:(NSString *)message;
+ (void)logFormat:(NSString *)format, ... NS_FORMAT_FUNCTION(1, 2);

/// 日志文件路径（Library/Caches/wbgw.log）。
+ (NSString *)logPath;

/// 给 Go 用的 fd（O_APPEND 打开，归 WBLog 所有，Go 只写不关）。
+ (int)logFD;

/// 读最后 N 行（非空行），供 UI 显示。
+ (NSArray<NSString *> *)tailLines:(NSUInteger)count;

@end

/// C 接口（给 Go 桥接与 C 代码用）。
int wb_log_fd(void);
NSArray<NSString *> *wb_log_tail(NSUInteger count);
void wb_log_msg(const char *msg);

NS_ASSUME_NONNULL_END
