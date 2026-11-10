#!/usr/bin/env python3
"""check_sandbox_paths.py — V12：新增的 Go/ObjC 代码不得直接调用
os.Getwd / os.UserHomeDir 做路径基准（注释除外）。

用法：python3 scripts/check_sandbox_paths.py [dir ...]
默认检查 go/ app/，外加 wb2api 树里本次新增的 Go 文件。
退出码：0 = 干净；1 = 发现调用。
"""
import pathlib
import re
import sys

PAT = re.compile(r"os\.(Getwd|UserHomeDir)\b")

DEFAULT_DIRS = ["go", "app"]
EXTRA_FILES = [
    "wb2api/cmd/server/boot.go",
    "wb2api/cmd/server/ioslib.go",
    "wb2api/cmd/server/sandbox_ios.go",
    "wb2api/cmd/server/sandbox_other.go",
    "wb2api/internal/selfupdate/selfupdate_ios.go",
    "wb2api/internal/ledger/ledger.go",
    "wb2api/cmd/server/listen.go",
]


def strip_comments(line):
    # 去掉 // 行注释与同一行内的 /* ... */ 块注释（近似处理，够 V12 用）。
    code = re.sub(r"//.*$", "", line)
    code = re.sub(r"/\*.*?\*/", "", code)
    return code


def check_file(path):
    bad = []
    try:
        text = pathlib.Path(path).read_text(errors="replace")
    except OSError as e:
        return [f"{path}: 读失败 {e}"]
    for i, line in enumerate(text.splitlines(), 1):
        if PAT.search(strip_comments(line)):
            bad.append(f"{path}:{i}: {line.strip()}")
    return bad


def main():
    roots = sys.argv[1:] or DEFAULT_DIRS
    files = []
    for r in roots:
        p = pathlib.Path(r)
        if p.is_dir():
            files += [x for x in p.rglob("*") if x.is_file()]
        elif p.is_file():
            files.append(p)
    for f in EXTRA_FILES:
        p = pathlib.Path(f)
        if p.is_file() and p not in files:
            files.append(p)
    bad = []
    for f in files:
        bad += check_file(f)
    if bad:
        print("V12 失败：发现直接调用 os.Getwd / os.UserHomeDir：")
        for b in bad:
            print("  " + b)
        return 1
    print(f"V12 通过：检查了 {len(files)} 个文件，无 os.Getwd / os.UserHomeDir 调用")
    return 0


if __name__ == "__main__":
    sys.exit(main())
