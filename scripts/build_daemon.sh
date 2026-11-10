#!/bin/sh
# build_daemon.sh — 越狱机 daemon 版二进制 out/daemon/wb2api-v9。
#
# 与宿主 App 共用同一份源码（internal/ledger + scheduler 接入零分叉），
# 只是入口不同：标准 server 入口 main()（launchd 拉起），不是 c-archive。
# 编译目标（任务书 §11）：CGO_ENABLED=1 GOOS=ios GOARCH=arm64，
# -ldflags '-linkmode external -extldflags=-fuse-ld=lld'。
# 产物需用户在越狱机上 ldid -S 后运行；本脚本不签名。
#
# instance_id 是配置值（config.instance_id）：App 侧默认 app-ios，
# 越狱机侧在其 config.json 里配 jb-iphone12，不编译进二进制。
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/wb2api"
OUT="$ROOT/out/daemon"
BIN="$OUT/wb2api-v9"
mkdir -p "$OUT"

# 工具链发现（GO_BIN、CLANG、LLVM_OBJDUMP）。
. "$ROOT/scripts/toolchain.sh"

: "${GO_BIN:?找不到 go：请把 go 1.26+ 放到 PATH，或设 GO_BIN}"
: "${CLANG:?找不到 clang：请安装 clang-18，或设 CLANG}"
# IOS_SDK：环境变量优先；未设时自动找本机常见位置（与 Makefile 一致）。
if [ -z "${IOS_SDK:-}" ]; then
    for d in "$HOME/workspace/cloudflared-ios-test/ios-sdks/iPhoneOS26.5.sdk" "$HOME/ios-sdk/iPhoneOS26.5.sdk" /opt/ios-sdk/iPhoneOS26.5.sdk; do
        if [ -d "$d" ]; then IOS_SDK="$d"; break; fi
    done
fi
: "${IOS_SDK:?请设 IOS_SDK，指向 iPhoneOS*.sdk（例：export IOS_SDK=/path/to/iPhoneOS26.5.sdk）}"
[ -d "$IOS_SDK" ] || { echo "IOS_SDK 不存在：$IOS_SDK" >&2; exit 1; }

echo "== build_daemon =="
"$GO_BIN" version
"$CLANG" --version | head -1
echo "IOS_SDK=$IOS_SDK"
echo "SRC=$SRC OUT=$OUT"

cd "$SRC"
export CGO_ENABLED=1
# 与 build_go.sh 路径 A 相同的 CC/CGO 旗标（iOS 15.0、lld 外部链接）。
GOOS=ios GOARCH=arm64 \
    CC="$CLANG -target arm64-apple-ios15.0 -isysroot $IOS_SDK" \
    CGO_CFLAGS="-isysroot $IOS_SDK -target arm64-apple-ios15.0" \
    CGO_LDFLAGS="-isysroot $IOS_SDK -target arm64-apple-ios15.0 -fuse-ld=lld" \
    "$GO_BIN" build -trimpath \
    -ldflags="-linkmode external -extldflags=-fuse-ld=lld" \
    -o "$BIN" ./cmd/server
echo "daemon 已产出：$BIN"
ls -l "$BIN"

# ---- 产物自检 ----
echo "== Mach-O 检查 =="
file "$BIN"
: "${LLVM_OBJDUMP:?找不到 llvm-objdump}"
# LC_BUILD_VERSION：platform 2（iOS）、minos 15.0。
if "$LLVM_OBJDUMP" --macho --private-headers "$BIN" 2>/dev/null | grep -q "platform ios"; then
    echo "  platform=ios（2）OK"
    "$LLVM_OBJDUMP" --macho --private-headers "$BIN" 2>/dev/null | grep -A4 "LC_BUILD_VERSION" | grep -E "platform|minos|sdk" | sed 's/^/    /'
else
    echo "错误：未确认 LC_BUILD_VERSION platform=ios" >&2
    exit 1
fi
echo "== 台账代码检查（V15） =="
N=$(grep -c 'claimFor\|taskledger' "$BIN" || true)
echo "  claimFor|taskledger 命中 $N 行"
if [ "${N:-0}" -gt 0 ]; then
    echo "  V15 OK"
else
    echo "错误：daemon 二进制不含台账代码" >&2
    exit 1
fi
echo "build_daemon 完成"
