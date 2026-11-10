// WBAudioKeepAlive.m — 静音音频保活（见 WBAudioKeepAlive.h）。
#import "WBAudioKeepAlive.h"
#import "WBLog.h"
#import <AVFoundation/AVFoundation.h>

static const AVAudioFrameCount kFrames = 1024;
static const NSInteger kMaxRetries = 5;
static const NSTimeInterval kBackoff[] = {0.5, 1.0, 2.0, 4.0};

@interface WBAudioKeepAlive ()
@property (nonatomic, strong) AVAudioEngine *engine;
@property (nonatomic, strong) AVAudioPlayerNode *player;
@property (nonatomic, strong) AVAudioPCMBuffer *silentBuffer;
@property (nonatomic, assign) NSInteger retryCount;
@property (nonatomic, assign) BOOL stopping;
@end

@implementation WBAudioKeepAlive

+ (instancetype)sharedKeepAlive {
    static WBAudioKeepAlive *s;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ s = [[WBAudioKeepAlive alloc] init]; });
    return s;
}

- (instancetype)init {
    self = [super init];
    if (self) {
        _retryCount = 0;
        _stopping = NO;
        NSNotificationCenter *nc = [NSNotificationCenter defaultCenter];
        [nc addObserver:self selector:@selector(onInterruption:) name:AVAudioSessionInterruptionNotification object:nil];
        [nc addObserver:self selector:@selector(onRouteChange:) name:AVAudioSessionRouteChangeNotification object:nil];
        [nc addObserver:self selector:@selector(onMediaServicesReset:) name:AVAudioSessionMediaServicesWereResetNotification object:nil];
    }
    return self;
}

- (void)dealloc {
    [[NSNotificationCenter defaultCenter] removeObserver:self];
}

// 全 0 PCM 缓冲：单声道、1024 帧。格式必须与 connect 时用的 format 同源，
// 否则 scheduleBuffer 会因格式不匹配抛异常（真机上 session 采样率常为
// 48kHz，不是写死的 44.1kHz）。
- (AVAudioPCMBuffer *)makeSilentBufferWithFormat:(AVAudioFormat *)format {
    AVAudioPCMBuffer *buf = [[AVAudioPCMBuffer alloc] initWithPCMFormat:format frameCapacity:kFrames];
    if (!buf) return nil;
    buf.frameLength = kFrames;
    // initWithPCMFormat 已把内存清零；显式再清一次，求稳。
    memset(buf.floatChannelData[0], 0, kFrames * sizeof(float));
    return buf;
}

- (BOOL)start {
    @try {
        if ([self isRunning]) return YES;
        self.stopping = NO;
        self.retryCount = 0;
        return [self startEngine];
    } @catch (NSException *e) {
        [WBLog logFormat:@"保活：启动异常（忽略，不影响网关）：%@", e.reason];
        return NO;
    }
}

- (BOOL)startEngine {
    NSError *err = nil;
    AVAudioSession *session = [AVAudioSession sharedInstance];
    // playback + 与其他音频混音（不抢占用户正在播的音乐）。
    if (![session setCategory:AVAudioSessionCategoryPlayback
                  withOptions:AVAudioSessionCategoryOptionMixWithOthers
                        error:&err]) {
        [WBLog logFormat:@"保活：设置音频 category 失败：%@", err.localizedDescription];
        return NO;
    }
    if (![session setActive:YES error:&err]) {
        [WBLog logFormat:@"保活：激活音频 session 失败：%@", err.localizedDescription];
        return NO;
    }
    if (!self.engine) {
        self.engine = [[AVAudioEngine alloc] init];
        self.player = [[AVAudioPlayerNode alloc] init];
        [self.engine attachNode:self.player];
        // 显式格式：用 session 实际采样率（真机常为 48kHz），connect 和
        // buffer 必须同源，否则 scheduleBuffer 会抛异常。
        AVAudioFormat *fmt = [[AVAudioFormat alloc] initStandardFormatWithSampleRate:session.sampleRate channels:1];
        if (!fmt) { [WBLog logMessage:@"保活：无法创建音频格式"]; return NO; }
        [self.engine connect:self.player to:self.engine.mainMixerNode format:fmt];
        self.silentBuffer = [self makeSilentBufferWithFormat:fmt];
        if (!self.silentBuffer) { [WBLog logMessage:@"保活：无法创建静音缓冲"]; return NO; }
    }
    if (![self.engine startAndReturnError:&err]) {
        [WBLog logFormat:@"保活：启动 AVAudioEngine 失败：%@", err.localizedDescription];
        return NO;
    }
    // 初始排 2 份；每份播完的 completion 里再补 1 份，队列恒为 2，无缝循环。
    [self scheduleBuffer];
    [self scheduleBuffer];
    [self.player play];
    [WBLog logMessage:@"保活：静音音频已启动"];
    return YES;
}

// 排一份静音缓冲；播完后自动补排下一份（队列里始终保持 2 份，无缝衔接）。
// 保活失败绝不能让 App 崩：scheduleBuffer 抛异常时停保活、App 继续跑。
- (void)scheduleBuffer {
    if (self.stopping || !self.silentBuffer) return;
    @try {
        __weak typeof(self) weakSelf = self;
        [self.player scheduleBuffer:self.silentBuffer completionHandler:^{
            __strong typeof(weakSelf) s = weakSelf;
            if (s && !s.stopping) [s scheduleBuffer];
        }];
    } @catch (NSException *e) {
        [WBLog logFormat:@"保活：排缓冲失败（已停保活，App 继续）：%@", e.reason];
        self.stopping = YES;
    }
}

- (void)stop {
    self.stopping = YES;
    if (self.player) [self.player stop];
    if (self.engine) [self.engine stop];
    NSError *err = nil;
    [[AVAudioSession sharedInstance] setActive:NO withOptions:AVAudioSessionSetActiveOptionNotifyOthersOnDeactivation error:&err];
    [WBLog logMessage:@"保活：静音音频已停止"];
}

- (BOOL)isRunning {
    return self.engine.isRunning && self.player.isPlaying;
}

// ---- 恢复：指数退避 0.5/1/2/4s，最多 5 次；成功后清零 ----
- (void)scheduleRestartWithReason:(NSString *)reason {
    if (self.stopping) return;
    if (self.retryCount >= kMaxRetries) {
        [WBLog logFormat:@"保活：%@ 后重试 %ld 次仍失败，放弃（下次前台再试）", reason, (long)kMaxRetries];
        return;
    }
    NSTimeInterval delay = kBackoff[MIN(self.retryCount, 3)];
    self.retryCount++;
    [WBLog logFormat:@"保活：%@，%.1fs 后第 %ld 次重试", reason, delay, (long)self.retryCount];
    __weak typeof(self) weakSelf = self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(delay * NSEC_PER_SEC)),
                   dispatch_get_main_queue(), ^{
        __strong typeof(weakSelf) strongSelf = weakSelf;
        if (!strongSelf || strongSelf.stopping) return;
        if ([strongSelf isRunning]) {
            strongSelf.retryCount = 0;
            return;
        }
        BOOL restarted = NO;
        @try {
            restarted = [strongSelf startEngine];
        } @catch (NSException *e) {
            [WBLog logFormat:@"保活：恢复启动异常（忽略，不影响网关）：%@", e.reason];
        }
        if (restarted) {
            strongSelf.retryCount = 0;
            [WBLog logFormat:@"保活：%@ 后恢复成功", reason];
        } else {
            [strongSelf scheduleRestartWithReason:reason];
        }
    });
}

- (void)onInterruption:(NSNotification *)note {
    NSNumber *type = note.userInfo[AVAudioSessionInterruptionTypeKey];
    if (type.integerValue == AVAudioSessionInterruptionTypeEnded) {
        // 中断结束：尝试恢复（系统可能已把 session 停掉）。
        [self scheduleRestartWithReason:@"音频中断结束"];
    } else {
        [WBLog logMessage:@"保活：音频被中断（开始）"];
    }
}

- (void)onRouteChange:(NSNotification *)note {
    NSNumber *reason = note.userInfo[AVAudioSessionRouteChangeReasonKey];
    // 旧设备断开 / 新设备接入 / 类别变化都可能导致引擎停，尝试恢复。
    if (reason.integerValue == AVAudioSessionRouteChangeReasonOldDeviceUnavailable ||
        reason.integerValue == AVAudioSessionRouteChangeReasonNewDeviceAvailable ||
        reason.integerValue == AVAudioSessionRouteChangeReasonCategoryChange) {
        [WBLog logFormat:@"保活：音频路由变化（reason=%ld）", (long)reason.integerValue];
        if (![self isRunning]) [self scheduleRestartWithReason:@"音频路由变化"];
    }
}

- (void)onMediaServicesReset:(NSNotification *)note {
    // 媒体服务重置：引擎已死，必须重建。
    [WBLog logMessage:@"保活：媒体服务已重置，重建引擎"];
    self.engine = nil;
    self.player = nil;
    self.silentBuffer = nil;
    [self scheduleRestartWithReason:@"媒体服务重置"];
}

@end
