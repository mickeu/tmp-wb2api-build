#!/bin/sh
# build_app.sh — ObjC 宿主 App → out/WB2Gateway.app（未签名）。
#
# 链接顺序（TASK.md v1.1 §9.2，已修正；v1.0 的顺序是错的）：
#   目标文件 → 静态库 → framework → 系统库
#   out/obj/*.o out/libwbgateway.a \
#     -framework UIKit -framework Foundation -framework AVFoundation \
#     -lresolv -lSystem \
#     -Wl,-platform_version -Wl,ios -Wl,15.0 -Wl,15.0
# 顺序错了会报几百行 Undefined symbols（只是顺序问题）。
# 本脚本不签名、不安装；只产出 .app bundle。
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/out"
APP="$OUT/WB2Gateway.app"
OBJ="$OUT/obj"
mkdir -p "$OBJ" "$APP"

# 工具链发现（CLANG、TMPDIR）。
. "$ROOT/scripts/toolchain.sh"

: "${IOS_SDK:?请设 IOS_SDK，指向 iPhoneOS*.sdk}"
[ -d "$IOS_SDK" ] || { echo "IOS_SDK 不存在：$IOS_SDK" >&2; exit 1; }
: "${CLANG:?找不到 clang：请安装 clang-18，或设 CLANG}"
[ -f "$OUT/libwbgateway.a" ] || { echo "缺少 $OUT/libwbgateway.a：先跑 make go" >&2; exit 1; }
[ -f "$OUT/libwbgateway.h" ] || { echo "缺少 $OUT/libwbgateway.h：先跑 make go" >&2; exit 1; }

echo "== build_app =="
"$CLANG" --version | head -1
echo "IOS_SDK=$IOS_SDK"

TARGET="arm64-apple-ios15.0"
# 编译参数说明：
# - 不用 -fmodules：Linux 上的 clang-18 读 iOS 26.5 SDK 的 modulemap 会报
#   "module '_c_standard_library_obsolete' requires feature
#   'found_incompatible_headers__check_search_paths'"（SDK 太新、clang 太旧），
#   纯 #import 不走模块系统，可正常编译。
# - -F SubFrameworks：UIKit 依赖的 UIUtilities.framework 在
#   System/Library/SubFrameworks/（不在默认的 Frameworks 搜索路径里），
#   不加会报 'UIUtilities/UIDefines.h' file not found。
CFLAGS="-target $TARGET -isysroot $IOS_SDK -F$IOS_SDK/System/Library/SubFrameworks -fobjc-arc -O2 -I$OUT -I$ROOT/app"

echo "== 编译 ObjC =="
for src in "$ROOT"/app/*.m; do
    name="$(basename "$src" .m)"
    echo "  clang -c $name.m"
    # shellcheck disable=SC2086
    $CLANG $CFLAGS -c "$src" -o "$OBJ/$name.o"
done

echo "== 链接（完整命令行，见 BUILD.log） =="
LINK_CMD="$CLANG -target $TARGET -isysroot $IOS_SDK -F$IOS_SDK/System/Library/SubFrameworks -fuse-ld=lld $OBJ/*.o $OUT/libwbgateway.a -framework UIKit -framework Foundation -framework AVFoundation -framework CoreGraphics -framework Security -lresolv -lSystem -Wl,-platform_version -Wl,ios -Wl,15.0 -Wl,15.0 -o $APP/WB2Gateway"
echo "LINK: $LINK_CMD"
# shellcheck disable=SC2086
$CLANG -target "$TARGET" -isysroot "$IOS_SDK" -F"$IOS_SDK/System/Library/SubFrameworks" -fuse-ld=lld \
    $OBJ/*.o "$OUT/libwbgateway.a" \
    -framework UIKit -framework Foundation -framework AVFoundation \
    -framework CoreGraphics -framework Security \
    -lresolv -lSystem \
    -Wl,-platform_version -Wl,ios -Wl,15.0 -Wl,15.0 \
    -o "$APP/WB2Gateway"
chmod 0755 "$APP/WB2Gateway"

echo "== 组装 bundle =="
cp "$ROOT/app/Info.plist" "$APP/Info.plist"
printf 'APPL????' > "$APP/PkgInfo"
cp "$ROOT/wb2api/config.example.json" "$APP/config.example.json"
chmod 0644 "$APP/Info.plist" "$APP/PkgInfo" "$APP/config.example.json"
echo "== 图标：Assets.car（三态 Any/Dark/Tinted）+ 旧式 PNG 兜底 =="
cp "$ROOT/app/Assets.car" "$APP/Assets.car"
python3 "$ROOT/scripts/gen_icons.py" "$APP"
ls -l "$APP"
echo "build_app 完成：$APP"
