// WBServer.h — Go 网关桥接：启动/停止/状态/面板查询。
//
// 配置路径：Application Support/wb2api/config.json。
// 首次启动若不存在，从 bundle 里的 config.example.json 复制一份。
// 状态查询（/panel/api/credit_today 与 /panel/api/pool）用 NSURLSession，
// 1.5 秒超时，且只在主线程之外执行，回调回主线程。
#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

/// 网关默认端口（冲突时 Go 侧按 +1/+2 降级）。
extern const NSInteger WBServerDefaultPort;

@interface WBServer : NSObject

+ (instancetype)sharedServer;

/// 配置文件路径（Application Support/wb2api/config.json）。
+ (NSString *)configPath;

/// 启动网关（幂等）。返回 YES = 已启动/已在运行，NO = 失败（见 lastError）。
- (BOOL)start;
/// 停止网关（幂等）。
- (void)stop;
/// 是否正在运行。
- (BOOL)isRunning;
/// 实际监听端口（未启动返回 0）。
- (NSInteger)port;
/// 最近一次错误（Go 侧缓存的只读字符串已拷走；无错误返回 nil）。
- (nullable NSString *)lastError;
/// 网关地址（http://127.0.0.1:PORT/v1），未启动时端口为默认端口。
- (NSString *)baseURLString;
/// 配置里的 api_key（读 config.json；缺失返回 @""）。
- (NSString *)apiKey;

/// 本地状态快照（running/port/address/error），同步返回。
- (NSDictionary *)statusSnapshot;

/// 面板状态（credit_today + overview），后台线程请求，1.5 秒超时，回调在主线程。
/// 结果：@{@"credit_today": id, @"overview": id, @"error": NSString*}（error 仅失败时有）。
/// 注意：面板接口要 Bearer 鉴权（api_key）；没有独立的 /panel/api/pool 路由，
/// 池计数与每账号状态在 /panel/api/overview 里。
- (void)fetchPanelStatusWithCompletion:(void (^)(NSDictionary *result))completion;

@end

NS_ASSUME_NONNULL_END
