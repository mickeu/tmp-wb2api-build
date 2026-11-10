// WBStatusViewController.h — 单页状态界面：网关状态 / 保活 / 今日积分 /
// 账号池（每 5 秒刷新）/ 日志最后 20 行；启动 / 停止按钮。
#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

@interface WBStatusViewController : UITableViewController

/// 立即刷新一次（启动时与回前台时调用）。
- (void)refreshStatus;

@end

NS_ASSUME_NONNULL_END
