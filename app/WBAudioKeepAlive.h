// WBAudioKeepAlive.h — 静音音频保活：后台不被系统挂起。
//
// AVAudioSession category playback + MixWithOthers；AVAudioEngine +
// AVAudioPlayerNode 循环播放一段全 0 的 PCM（44100Hz、单声道、1024 帧）。
// 循环方式：初始排 2 份缓冲，每份播完的 completion 里再补排 1 份——
// 队列里始终有 2 份在等，无缝衔接、无需定时器、队列不无限增长
//（等价于“0.9 倍缓冲时长补调度”的效果，播完即补）。
// 中断 / 路由变化 / 媒体服务重置三类通知触发恢复，指数退避
// 0.5s → 1s → 2s → 4s，最多 5 次；恢复成功后退避清零。
#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

@interface WBAudioKeepAlive : NSObject

+ (instancetype)sharedKeepAlive;

/// 启动保活（幂等）。YES = 音频已在播。
- (BOOL)start;
/// 停止保活（幂等）。
- (void)stop;
/// 是否正在播放静音音频。
- (BOOL)isRunning;

@end

NS_ASSUME_NONNULL_END
