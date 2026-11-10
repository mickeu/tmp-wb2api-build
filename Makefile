# Makefile — wb2api iOS 宿主 App 构建入口。
#
#   make go      Go 交叉编译 → out/libwbgateway.a（路径 A 优先，失败切路径 B）
#   make daemon  越狱机 daemon 版 → out/daemon/wb2api-v9（GOOS=ios 常规可执行文件）
#   make app     ObjC 编译 + 链接 → out/WB2Gateway.app（未签名）
#   make ipa     打包 → out/WB2Gateway-unsigned.ipa（未签名）
#   make patch   生成 git-diff.patch（iOS 改造对修复后基线的 diff）
#   make verify  静态验收 V1–V12（全部通过才 exit 0）
#   make all     全部（go → app → ipa → patch → verify）
#   make clean   清理 out/（保留源码与交付物）
#
# 环境变量：
#   IOS_SDK  iPhoneOS*.sdk 的路径（必填；未设时自动找本机常见位置）。
#   GO_BIN   go 可执行文件（默认 PATH 里的 go，其次 ~/workspace/go1.26.10/bin/go）。
#   CLANG    clang（默认 clang-18）。
#   C_ARCHIVE_PATH  强制 c-archive 路径 A 或 B（默认自动：A 不支持时切 B）。
#
# BUILD.log：跑 `make all > BUILD.log 2>&1`，里面含 go version、clang --version、
# IOS_SDK 实际路径与完整链接命令行（info 目标与 build_app.sh 输出）。

ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

# IOS_SDK：优先用环境变量；未设时自动找本机常见位置。
IOS_SDK ?= $(firstword $(wildcard \
	$(HOME)/workspace/cloudflared-ios-test/ios-sdks/iPhoneOS26.5.sdk \
	$(HOME)/ios-sdk/iPhoneOS26.5.sdk \
	/opt/ios-sdk/iPhoneOS26.5.sdk))
export IOS_SDK

.PHONY: all go app ipa patch verify clean info

all: info go daemon app ipa patch verify

info:
	@echo "== wb2api-host 构建信息 =="
	@echo "ROOT=$(ROOT)"
	@echo "IOS_SDK=$(IOS_SDK)"
	@. "$(ROOT)/scripts/toolchain.sh"; \
		if [ -n "$${GO_BIN:-}" ]; then "$${GO_BIN}" version; else echo "go: 未找到（设 GO_BIN 或把 go 1.26+ 放进 PATH）"; fi; \
		if [ -n "$${CLANG:-}" ]; then "$${CLANG}" --version | head -1; else echo "clang: 未找到（安装 clang-18 或设 CLANG）"; fi

go:
	sh scripts/build_go.sh

daemon:
	sh scripts/build_daemon.sh

app:
	sh scripts/build_app.sh

ipa:
	python3 scripts/pack_ipa.py

patch:
	python3 scripts/gen_patch.py git-diff.patch

verify:
	sh scripts/verify.sh

clean:
	rm -rf out
	mkdir -p out
