#!/bin/sh
# build_go.sh — Go 网关 → out/libwbgateway.a（c-archive）+ out/libwbgateway.h。
#
# 路径 A（优先）：GOOS=ios + -buildmode=c-archive。
# 路径 B（A 报 "not supported on ios" 时）：GOOS=darwin + -buildmode=c-archive，
#   再用 scripts/patch_platform.py 把 LC_BUILD_VERSION 的 platform 改成 2（iOS）、minos 15.0。
#
# 注意：
# - CC 里不要放 -fuse-ld=lld：cgo 编译时会把它当成编译参数，clang 以
#   -Werror 报 "argument unused during compilation"。链接器选择靠
#   -ldflags "-linkmode external -extldflags=-fuse-ld=lld" 传入。
# - 首次 c-archive 链接耗时 5–15 分钟属正常（CGO 包装 + 全量目标文件解析），不要掐断。
# - /tmp 可能是 512M 的小 tmpfs；Go 链接临时文件很大，改到 $HOME 下的 TMPDIR。
# - 本脚本不签名、不安装，只产出 .a + .h。
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/wb2api"
OUT="$ROOT/out"
LOG="$OUT/build_go.log"
mkdir -p "$OUT"

# 工具链发现（GO_BIN、CLANG、LLVM_NM、TMPDIR）。
. "$ROOT/scripts/toolchain.sh"

: "${GO_BIN:?找不到 go：请把 go 1.26+ 放到 PATH，或设 GO_BIN}"
: "${CLANG:?找不到 clang：请安装 clang-18，或设 CLANG}"

: "${IOS_SDK:?请设 IOS_SDK，指向 iPhoneOS*.sdk（例：export IOS_SDK=/path/to/iPhoneOS26.5.sdk）}"
[ -d "$IOS_SDK" ] || { echo "IOS_SDK 不存在：$IOS_SDK" >&2; exit 1; }

echo "== build_go =="
"$GO_BIN" version
"$CLANG" --version | head -1
echo "IOS_SDK=$IOS_SDK"
echo "GO_BIN=$GO_BIN CLANG=$CLANG"
echo "SRC=$SRC OUT=$OUT"

# ---- 把 go/ 里的新增文件同步进源码树（幂等）----
cp "$ROOT/go/ioslib.go" "$SRC/cmd/server/ioslib.go"
cp "$ROOT/go/selfupdate_ios.go" "$SRC/internal/selfupdate/selfupdate_ios.go"

cd "$SRC"
export CGO_ENABLED=1
export GOFLAGS="${GOFLAGS:-}"
COMMON_CFLAGS="-isysroot $IOS_SDK -target arm64-apple-ios15.0"
COMMON_LDFLAGS="-isysroot $IOS_SDK -target arm64-apple-ios15.0 -fuse-ld=lld"

# ---- 路径 A：GOOS=ios ----
echo "== 路径 A：GOOS=ios -buildmode=c-archive =="
set +e
GOOS=ios GOARCH=arm64 \
    CC="$CLANG -target arm64-apple-ios15.0 -isysroot $IOS_SDK" \
    CGO_CFLAGS="$COMMON_CFLAGS" \
    CGO_LDFLAGS="$COMMON_LDFLAGS" \
    "$GO_BIN" build -buildmode=c-archive \
    -ldflags="-linkmode external -extldflags=-fuse-ld=lld" \
    -o "$OUT/libwbgateway.a" ./cmd/server >"$LOG" 2>&1
rc=$?
set -e
if [ $rc -eq 0 ]; then
    echo "路径 A 成功：$OUT/libwbgateway.a"
    echo "C_ARCHIVE_PATH=A"
else
    echo "路径 A 失败（exit $rc），见 $LOG 末尾："
    tail -20 "$LOG"
    if grep -q "not supported on ios" "$LOG"; then
        echo "== 路径 B：GOOS=darwin -buildmode=c-archive + patch platform =="
        # 路径 B 的 C 编译目标用 macOS（这样产出的 Mach-O 才是 platform=1，
        # 再由 patch_platform.py 统一改成 iOS）；SDK 只有 iPhoneOS 可用，
        # C 部分只用基础 Darwin 头文件（stdlib/mach），iOS SDK 里都有。
        GOOS=darwin GOARCH=arm64 \
            CC="$CLANG -target arm64-apple-macosx15.0 -isysroot $IOS_SDK" \
            CGO_CFLAGS="-isysroot $IOS_SDK -target arm64-apple-macosx15.0" \
            CGO_LDFLAGS="-isysroot $IOS_SDK -target arm64-apple-macosx15.0 -fuse-ld=lld" \
            "$GO_BIN" build -buildmode=c-archive \
            -ldflags="-linkmode external -extldflags=-fuse-ld=lld" \
            -o "$OUT/libwbgateway.a" ./cmd/server >>"$LOG" 2>&1
        echo "路径 B 编译完成，改 LC_BUILD_VERSION platform 1→2、minos→15.0 …"
        python3 "$ROOT/scripts/patch_platform.py" "$OUT/libwbgateway.a" >>"$LOG" 2>&1
        echo "路径 B 成功：$OUT/libwbgateway.a（已 patch）"
        echo "C_ARCHIVE_PATH=B"
    else
        echo "路径 A 失败原因不是 not supported，不自动切路径 B（见 $LOG 全文）" >&2
        exit $rc
    fi
fi

# ---- 产物自检：四个导出符号必须可见 ----
# 先跑 ranlib：Go 的 c-archive 产物没有符号索引（__.SYMDEF），
# ld64.lld 会报 "archive has no index" 并找不到 WBG_* 符号。
# llvm-ranlib 加的是 GNU 格式索引（首成员 "/"），ld64.lld 认它。
echo "== 加符号索引（ranlib） =="
: "${LLVM_RANLIB:?找不到 llvm-ranlib}"
"$LLVM_RANLIB" "$OUT/libwbgateway.a"
echo "== 导出符号检查 =="
"$LLVM_NM" -g "$OUT/libwbgateway.a" | grep -E ' _WBG_(Start|Stop|Port|LastError)$' || {
    echo "错误：WBG_* 符号缺失" >&2; exit 1
}
ls -l "$OUT/libwbgateway.a" "$OUT/libwbgateway.h"
echo "build_go 完成"
