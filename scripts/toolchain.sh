# toolchain.sh — 工具链发现（供 build_go.sh / build_app.sh / verify.sh source）。
# 设置：GO_BIN、CLANG、LLVM_NM、LLVM_OBJDUMP、IOS_SDK、TMPDIR。
# 环境变量已设时优先用环境变量；找不到时由调用方报错。
# 用法：. "$(dirname "$0")/toolchain.sh"

# /tmp 可能是 512M 的小 tmpfs；Go 链接临时文件很大，改到 $HOME 下。
export TMPDIR="${WB2API_TMPDIR:-$HOME/.wb2api-tmp}"
mkdir -p "$TMPDIR" 2>/dev/null || true

# clang 的 -fuse-ld=lld 要找名叫 ld.lld 的链接器：它只在 clang 所在目录和
# PATH 里找。Ubuntu 上 ld.lld 在 /usr/lib/llvm-18/bin（不在 PATH；lld-18
# 包只提供 /usr/bin/ld.lld-18，不提供未加后缀的 ld.lld），所以把该目录加进
# PATH。没有这一步，-fuse-ld=lld 会报 "invalid linker name"。
if [ -d /usr/lib/llvm-18/bin ]; then
    case ":$PATH:" in
        *:/usr/lib/llvm-18/bin:*) ;;
        *) PATH="/usr/lib/llvm-18/bin:$PATH"; export PATH ;;
    esac
fi

if [ -z "${GO_BIN:-}" ]; then
    if command -v go >/dev/null 2>&1; then
        GO_BIN="$(command -v go)"
    else
        for c in "$HOME/workspace/go1.26.10/bin/go" "$HOME/workspace/cloudflared-ios-test/go1.25.3/bin/go"; do
            if [ -x "$c" ]; then GO_BIN="$c"; break; fi
        done
    fi
fi

if [ -z "${CLANG:-}" ]; then
    CLANG="$(command -v clang-18 || command -v clang || true)"
fi

if [ -z "${LLVM_NM:-}" ]; then
    LLVM_NM="$(command -v llvm-nm-18 || command -v llvm-nm || echo /usr/lib/llvm-18/bin/llvm-nm)"
fi

if [ -z "${LLVM_OBJDUMP:-}" ]; then
    LLVM_OBJDUMP="$(command -v llvm-objdump-18 || command -v llvm-objdump || echo /usr/lib/llvm-18/bin/llvm-objdump)"
fi

if [ -z "${LLVM_RANLIB:-}" ]; then
    LLVM_RANLIB="$(command -v llvm-ranlib-18 || command -v llvm-ranlib || command -v ranlib || echo /usr/lib/llvm-18/bin/llvm-ranlib)"
fi

# IOS_SDK 由调用方校验（不存在时报错）。
