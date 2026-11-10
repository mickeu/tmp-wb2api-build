// WBStatusViewController.m — 单页状态界面（见 .h）。
//
// 分区：0 网关 / 1 保活 / 2 今日积分 / 3 账号池 / 4 日志。
// 账号池与今日积分每 5 秒刷新（NSTimer）；回前台与收到
// WB2GatewayRefreshStatus 通知时立即刷新；支持下拉刷新。
// 地址与 API Key 点按即复制到剪贴板。
#import "WBStatusViewController.h"
#import "WBLog.h"
#import "WBServer.h"
#import "WBAudioKeepAlive.h"

static const NSTimeInterval kRefreshInterval = 5.0;

@interface WBStatusViewController ()
@property (nonatomic, strong) NSTimer *timer;
@property (nonatomic, strong) NSDictionary *panelStatus; // credit_today + overview
@property (nonatomic, strong) NSArray<NSString *> *logLines;
@property (nonatomic, strong) UIButton *startButton;
@property (nonatomic, strong) UIButton *stopButton;
@end

@implementation WBStatusViewController

- (instancetype)init {
    self = [super initWithStyle:UITableViewStyleInsetGrouped];
    return self;
}

- (void)viewDidLoad {
    [super viewDidLoad];
    self.title = @"WB2Gateway";
    self.navigationItem.title = @"WB2Gateway";
    // 底部启动/停止按钮。
    UIView *footer = [[UIView alloc] initWithFrame:CGRectMake(0, 0, 0, 64)];
    self.startButton = [UIButton buttonWithType:UIButtonTypeSystem];
    [self.startButton setTitle:@"启动网关" forState:UIControlStateNormal];
    self.startButton.titleLabel.font = [UIFont boldSystemFontOfSize:17];
    [self.startButton addTarget:self action:@selector(onStart) forControlEvents:UIControlEventTouchUpInside];
    self.stopButton = [UIButton buttonWithType:UIButtonTypeSystem];
    [self.stopButton setTitle:@"停止网关" forState:UIControlStateNormal];
    self.stopButton.titleLabel.font = [UIFont systemFontOfSize:17];
    [self.stopButton addTarget:self action:@selector(onStop) forControlEvents:UIControlEventTouchUpInside];
    // 左右并排。
    self.startButton.translatesAutoresizingMaskIntoConstraints = NO;
    self.stopButton.translatesAutoresizingMaskIntoConstraints = NO;
    [footer addSubview:self.startButton];
    [footer addSubview:self.stopButton];
    [NSLayoutConstraint activateConstraints:@[
        [self.startButton.leadingAnchor constraintEqualToAnchor:footer.leadingAnchor constant:20],
        [self.startButton.centerYAnchor constraintEqualToAnchor:footer.centerYAnchor],
        [self.startButton.widthAnchor constraintEqualToAnchor:footer.widthAnchor multiplier:0.42],
        [self.stopButton.trailingAnchor constraintEqualToAnchor:footer.trailingAnchor constant:-20],
        [self.stopButton.centerYAnchor constraintEqualToAnchor:footer.centerYAnchor],
        [self.stopButton.widthAnchor constraintEqualToAnchor:footer.widthAnchor multiplier:0.42],
    ]];
    self.tableView.tableFooterView = footer;
    // 下拉刷新。
    UIRefreshControl *rc = [[UIRefreshControl alloc] init];
    [rc addTarget:self action:@selector(refreshStatus) forControlEvents:UIControlEventValueChanged];
    self.refreshControl = rc;
    // 回前台刷新（AppDelegate 也会发这个通知）。
    [[NSNotificationCenter defaultCenter] addObserver:self
                                             selector:@selector(refreshStatus)
                                                 name:@"WB2GatewayRefreshStatus"
                                               object:nil];
    [[NSNotificationCenter defaultCenter] addObserver:self
                                             selector:@selector(refreshStatus)
                                                 name:UIApplicationWillEnterForegroundNotification
                                               object:nil];
}

- (void)viewDidAppear:(BOOL)animated {
    [super viewDidAppear:animated];
    [self refreshStatus];
    [self.timer invalidate];
    self.timer = [NSTimer scheduledTimerWithTimeInterval:kRefreshInterval
                                                  target:self
                                                selector:@selector(refreshStatus)
                                                userInfo:nil
                                                 repeats:YES];
}

- (void)viewDidDisappear:(BOOL)animated {
    [super viewDidDisappear:animated];
    [self.timer invalidate];
    self.timer = nil;
}

- (void)dealloc {
    [[NSNotificationCenter defaultCenter] removeObserver:self];
    [self.timer invalidate];
}

// ---- 操作 ----
- (void)onStart {
    [[WBServer sharedServer] start];
    [[WBAudioKeepAlive sharedKeepAlive] start];
    [self refreshStatus];
}

- (void)onStop {
    [[WBServer sharedServer] stop];
    [[WBAudioKeepAlive sharedKeepAlive] stop];
    [self refreshStatus];
}

- (void)refreshStatus {
    WBServer *srv = [WBServer sharedServer];
    // 本地状态立即更新；面板状态异步回填。
    self.logLines = [WBLog tailLines:20];
    [srv fetchPanelStatusWithCompletion:^(NSDictionary *result) {
        self.panelStatus = result;
        [self.tableView reloadData];
        [self.refreshControl endRefreshing];
    }];
    [self.tableView reloadData];
}

// ---- 小工具 ----
- (NSString *)maskedKey:(NSString *)key {
    if (key.length <= 8) return key.length ? key : @"（未设置）";
    return [NSString stringWithFormat:@"%@••••••••", [key substringToIndex:8]];
}

// 把任意 JSON 值压成一行可读文本（面板返回形状变化时不崩）。
- (NSString *)compactJSON:(id)obj {
    if (!obj || obj == [NSNull null]) return @"—";
    if ([obj isKindOfClass:[NSNumber class]]) return [obj stringValue];
    if ([obj isKindOfClass:[NSString class]]) return obj;
    if ([obj isKindOfClass:[NSArray class]]) {
        return [NSString stringWithFormat:@"%lu 项", (unsigned long)[(NSArray *)obj count]];
    }
    if ([obj isKindOfClass:[NSDictionary class]]) {
        NSDictionary *d = obj;
        NSMutableArray *parts = [NSMutableArray array];
        for (NSString *k in d) {
            id v = d[k];
            if ([v isKindOfClass:[NSNumber class]] || [v isKindOfClass:[NSString class]]) {
                [parts addObject:[NSString stringWithFormat:@"%@: %@", k, v]];
            }
        }
        return parts.count ? [parts componentsJoinedByString:@"  "] : @"—";
    }
    return [obj description];
}

- (void)copyToPasteboard:(NSString *)text label:(NSString *)label {
    if (!text.length) return;
    [UIPasteboard generalPasteboard].string = text;
    [WBLog logFormat:@"已复制%@到剪贴板", label];
}

// ---- Table ----
- (NSInteger)numberOfSectionsInTableView:(UITableView *)tableView { return 5; }

- (NSString *)tableView:(UITableView *)tableView titleForHeaderInSection:(NSInteger)section {
    switch (section) {
        case 0: return @"网关";
        case 1: return @"保活";
        case 2: return @"今日积分";
        case 3: return @"账号池";
        case 4: return @"日志（最后 20 行）";
        default: return nil;
    }
}

- (NSInteger)tableView:(UITableView *)tableView numberOfRowsInSection:(NSInteger)section {
    switch (section) {
        case 0: return 4; // 状态 / 端口 / 地址 / API Key
        case 1: return 1;
        case 2: return 1;
        case 3: {
            NSArray *accts = self.panelStatus[@"overview"][@"accounts"];
            if ([accts isKindOfClass:[NSArray class]] && accts.count) return accts.count;
            return 1;
        }
        case 4: return 1;
        default: return 0;
    }
}

- (UITableViewCell *)gatewayCellForRow:(NSInteger)row {
    static NSString *reuse = @"gw";
    UITableViewCell *cell = [self.tableView dequeueReusableCellWithIdentifier:reuse];
    if (!cell) cell = [[UITableViewCell alloc] initWithStyle:UITableViewCellStyleValue1 reuseIdentifier:reuse];
    WBServer *srv = [WBServer sharedServer];
    BOOL running = [srv isRunning];
    switch (row) {
        case 0:
            cell.textLabel.text = @"状态";
            cell.detailTextLabel.text = running ? @"运行中" : @"已停止";
            cell.detailTextLabel.textColor = running ? [UIColor systemGreenColor] : [UIColor systemRedColor];
            cell.accessoryType = UITableViewCellAccessoryNone;
            break;
        case 1:
            cell.textLabel.text = @"端口";
            cell.detailTextLabel.text = running ? [NSString stringWithFormat:@"%ld", (long)[srv port]] : @"—";
            cell.detailTextLabel.textColor = [UIColor labelColor];
            cell.accessoryType = UITableViewCellAccessoryNone;
            break;
        case 2:
            cell.textLabel.text = @"地址";
            cell.detailTextLabel.text = [srv baseURLString];
            cell.detailTextLabel.textColor = [UIColor labelColor];
            cell.accessoryType = UITableViewCellAccessoryDisclosureIndicator;
            break;
        case 3: {
            cell.textLabel.text = @"API Key";
            cell.detailTextLabel.text = [self maskedKey:[srv apiKey]];
            cell.detailTextLabel.textColor = [UIColor labelColor];
            cell.accessoryType = UITableViewCellAccessoryDisclosureIndicator;
            break;
        }
    }
    return cell;
}

- (UITableViewCell *)tableView:(UITableView *)tableView cellForRowAtIndexPath:(NSIndexPath *)indexPath {
    if (indexPath.section == 0) return [self gatewayCellForRow:indexPath.row];
    if (indexPath.section == 1) {
        static NSString *reuse = @"ka";
        UITableViewCell *cell = [tableView dequeueReusableCellWithIdentifier:reuse];
        if (!cell) cell = [[UITableViewCell alloc] initWithStyle:UITableViewCellStyleValue1 reuseIdentifier:reuse];
        BOOL alive = [[WBAudioKeepAlive sharedKeepAlive] isRunning];
        cell.textLabel.text = @"静音音频";
        cell.detailTextLabel.text = alive ? @"运行中" : @"已停止";
        cell.detailTextLabel.textColor = alive ? [UIColor systemGreenColor] : [UIColor secondaryLabelColor];
        return cell;
    }
    if (indexPath.section == 2) {
        static NSString *reuse = @"cr";
        UITableViewCell *cell = [tableView dequeueReusableCellWithIdentifier:reuse];
        if (!cell) cell = [[UITableViewCell alloc] initWithStyle:UITableViewCellStyleValue1 reuseIdentifier:reuse];
        cell.textLabel.text = @"今日";
        cell.detailTextLabel.text = [self compactJSON:self.panelStatus[@"credit_today"]];
        cell.detailTextLabel.textColor = [UIColor labelColor];
        return cell;
    }
    if (indexPath.section == 3) {
        static NSString *reuse = @"pool";
        UITableViewCell *cell = [tableView dequeueReusableCellWithIdentifier:reuse];
        if (!cell) cell = [[UITableViewCell alloc] initWithStyle:UITableViewCellStyleValue1 reuseIdentifier:reuse];
        NSArray *accts = self.panelStatus[@"overview"][@"accounts"];
        if ([accts isKindOfClass:[NSArray class]] && accts.count) {
            NSDictionary *a = accts[indexPath.row];
            NSString *name = a[@"nickname"];
            if (![name isKindOfClass:[NSString class]] || !name.length) name = a[@"uid"];
            if (![name isKindOfClass:[NSString class]]) name = @"（未知账号）";
            NSString *status;
            if ([a[@"disabled"] boolValue]) {
                status = [NSString stringWithFormat:@"已禁用%@", a[@"disabled_reason"] ?: @""];
            } else if ([a[@"cooling"] boolValue]) {
                status = @"冷却中";
            } else {
                status = @"正常";
            }
            cell.textLabel.text = name;
            cell.detailTextLabel.text = [NSString stringWithFormat:@"积分 %@ · %@", a[@"credits"] ?: @"—", status];
        } else {
            cell.textLabel.text = self.panelStatus[@"error"] ?: @"暂无账号数据";
            cell.detailTextLabel.text = @"";
        }
        cell.detailTextLabel.textColor = [UIColor labelColor];
        return cell;
    }
    // section 4：日志
    static NSString *reuse = @"log";
    UITableViewCell *cell = [tableView dequeueReusableCellWithIdentifier:reuse];
    UITextView *tv = nil;
    if (!cell) {
        cell = [[UITableViewCell alloc] initWithStyle:UITableViewCellStyleDefault reuseIdentifier:reuse];
        cell.selectionStyle = UITableViewCellSelectionStyleNone;
        tv = [[UITextView alloc] initWithFrame:CGRectZero];
        tv.tag = 100;
        tv.editable = NO;
        tv.scrollEnabled = NO;
        tv.font = [UIFont monospacedSystemFontOfSize:11 weight:UIFontWeightRegular];
        tv.textColor = [UIColor secondaryLabelColor];
        tv.backgroundColor = [UIColor clearColor];
        tv.translatesAutoresizingMaskIntoConstraints = NO;
        [cell.contentView addSubview:tv];
        [NSLayoutConstraint activateConstraints:@[
            [tv.topAnchor constraintEqualToAnchor:cell.contentView.topAnchor constant:8],
            [tv.bottomAnchor constraintEqualToAnchor:cell.contentView.bottomAnchor constant:-8],
            [tv.leadingAnchor constraintEqualToAnchor:cell.contentView.leadingAnchor constant:16],
            [tv.trailingAnchor constraintEqualToAnchor:cell.contentView.trailingAnchor constant:-16],
        ]];
    } else {
        tv = [cell.contentView viewWithTag:100];
    }
    tv.text = self.logLines.count ? [self.logLines componentsJoinedByString:@"\n"] : @"（暂无日志）";
    return cell;
}

- (CGFloat)tableView:(UITableView *)tableView heightForRowAtIndexPath:(NSIndexPath *)indexPath {
    if (indexPath.section == 4) return 220;
    return UITableViewAutomaticDimension;
}

- (NSString *)tableView:(UITableView *)tableView titleForFooterInSection:(NSInteger)section {
    if (section == 3) {
        NSDictionary *ov = self.panelStatus[@"overview"];
        if ([ov isKindOfClass:[NSDictionary class]]) {
            return [NSString stringWithFormat:@"共 %@ 个 · 健康 %@ · 冷却 %@ · 停用 %@",
                    ov[@"total"] ?: @"—", ov[@"healthy"] ?: @"—",
                    ov[@"cooling"] ?: @"—", ov[@"disabled"] ?: @"—"];
        }
    }
    return nil;
}

- (void)tableView:(UITableView *)tableView didSelectRowAtIndexPath:(NSIndexPath *)indexPath {
    [tableView deselectRowAtIndexPath:indexPath animated:YES];
    WBServer *srv = [WBServer sharedServer];
    if (indexPath.section == 0 && indexPath.row == 2) {
        [self copyToPasteboard:[srv baseURLString] label:@"地址"];
    } else if (indexPath.section == 0 && indexPath.row == 3) {
        [self copyToPasteboard:[srv apiKey] label:@"API Key"];
    }
}

@end
