#!/usr/bin/env python3
"""gen_patch.py — 生成 git-diff.patch（iOS 改造对修复后源码基线的全部改动）。

基线 = 上游 v1.13.0 + 5 个生产补丁（见 patches/production-5.patch）。
改动由三部分组成：
  1. 修改的文件：patches/pristine/ 里存有基线版，当前 wb2api/ 树里是改后版。
  2. 新增的文件：直接列在 NEW_FILES 里（基线不存在）。
  3. 宿主侧修改的文件：app/ 下的宿主代码（基线在 patches/pristine/app/）。
输出为 git 风格 unified diff（a/ b/ 路径以 wb2api/ 树为根；宿主文件以仓库根为根）。

用法：python3 scripts/gen_patch.py [输出路径，默认 git-diff.patch]
"""
import difflib
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
WB2API = ROOT / "wb2api"
PRISTINE = ROOT / "patches" / "pristine"

# 基线存在、被 iOS 改造改过的文件（相对 wb2api/）。
MODIFIED = [
    "cmd/server/main.go",
    "cmd/server/config.go",
    "cmd/server/config_test.go",
    "config.example.json",
    "internal/selfupdate/selfupdate.go",
    "internal/selfupdate/install.go",
    "internal/server/handler.go",
    "internal/server/handler_test.go",
    "internal/scheduler/scheduler.go",
    "internal/scheduler/travel.go",
    "internal/scheduler/blackcat.go",
    "internal/panel/taskcenter.go",
]
# iOS 改造新增的文件（相对 wb2api/）。
NEW_FILES = [
    "cmd/server/boot.go",
    "cmd/server/ioslib.go",
    "cmd/server/sandbox_ios.go",
    "cmd/server/sandbox_other.go",
    "internal/selfupdate/selfupdate_ios.go",
    "internal/ledger/ledger.go",
    "internal/ledger/ledger_test.go",
    "internal/scheduler/ledger_test.go",
    "cmd/server/listen.go",
    "cmd/server/listen_test.go",
]
# 宿主侧修改的文件（相对仓库根；基线在 patches/pristine/<rel>）。
HOST_MODIFIED = [
    "app/WBServer.m",
    "app/WBAppDelegate.m",
    "app/WBServer.h",
    "app/Info.plist",
    "Makefile",
    "scripts/build_app.sh",
]
# 宿主侧新增的文件（相对仓库根；无基线，直接整文件进 patch）。
HOST_NEW_FILES = [
    "scripts/build_daemon.sh",
    "scripts/gen_icons.py",
]


def git_diff_modified(rel, old_path, new_path):
    old = old_path.read_text()
    new = new_path.read_text()
    lines = difflib.unified_diff(
        old.splitlines(keepends=True), new.splitlines(keepends=True),
        fromfile=f"a/{rel}", tofile=f"b/{rel}")
    out = [f"diff --git a/{rel} b/{rel}"]
    out += [l.rstrip("\n") for l in lines]
    return "\n".join(out) + "\n"


def git_diff_new(rel, new_path):
    new = new_path.read_text()
    lines = difflib.unified_diff(
        [], new.splitlines(keepends=True),
        fromfile=f"a/{rel}", tofile=f"b/{rel}")
    out = [f"diff --git a/{rel} b/{rel}",
           "new file mode 100644"]
    out += [l.rstrip("\n") for l in lines]
    return "\n".join(out) + "\n"


def main():
    out_path = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "git-diff.patch"
    missing = [r for r in MODIFIED if not (PRISTINE / r).is_file()]
    if missing:
        raise SystemExit(f"缺少基线文件 patches/pristine/：{missing}")
    missing = [r for r in HOST_MODIFIED if not (PRISTINE / r).is_file()]
    if missing:
        raise SystemExit(f"缺少宿主基线文件 patches/pristine/：{missing}")
    missing = [r for r in MODIFIED + NEW_FILES if not (WB2API / r).is_file()]
    if missing:
        raise SystemExit(f"wb2api/ 里缺少文件：{missing}")
    missing = [r for r in HOST_MODIFIED if not (ROOT / r).is_file()]
    if missing:
        raise SystemExit(f"宿主文件缺失：{missing}")
    missing = [r for r in HOST_NEW_FILES if not (ROOT / r).is_file()]
    if missing:
        raise SystemExit(f"宿主新文件缺失：{missing}")
    parts = []
    for rel in MODIFIED:
        d = git_diff_modified(rel, PRISTINE / rel, WB2API / rel)
        if d.count("\n") > 1:
            parts.append(d)
    for rel in NEW_FILES:
        parts.append(git_diff_new(rel, WB2API / rel))
    for rel in HOST_MODIFIED:
        d = git_diff_modified(rel, PRISTINE / rel, ROOT / rel)
        if d.count("\n") > 1:
            parts.append(d)
    for rel in HOST_NEW_FILES:
        parts.append(git_diff_new(rel, ROOT / rel))
    out_path.write_text("".join(parts))
    print(f"git-diff.patch 已生成：{out_path}（{len(parts)} 个文件）")
    for rel in MODIFIED + NEW_FILES + HOST_MODIFIED + HOST_NEW_FILES:
        print(f"  {rel}")


if __name__ == "__main__":
    main()
