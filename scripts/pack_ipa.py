#!/usr/bin/env python3
"""pack_ipa.py — out/WB2Gateway.app → out/WB2Gateway-unsigned.ipa（未签名）。

只用标准库 zipfile。结构必须是 Payload/WB2Gateway.app/...（Payload 大小写敏感）。
权限：目录与可执行文件 0755，其余文件 0644（ZipInfo.external_attr）。
"""
import os
import stat
import sys
import zipfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
APP = os.path.join(ROOT, "out", "WB2Gateway.app")
IPA = os.path.join(ROOT, "out", "WB2Gateway-unsigned.ipa")


def main():
    if not os.path.isdir(APP):
        raise SystemExit(f"缺少 {APP}：先跑 make app")
    if not os.path.isfile(os.path.join(APP, "WB2Gateway")):
        raise SystemExit(f"缺少 {APP}/WB2Gateway：先跑 make app")
    if os.path.exists(IPA):
        os.remove(IPA)
    count = 0
    with zipfile.ZipFile(IPA, "w", zipfile.ZIP_DEFLATED) as z:
        for dirpath, dirnames, filenames in os.walk(APP):
            dirnames.sort()
            # 先写目录项（保证解包顺序与权限）。
            rel_dir = os.path.relpath(dirpath, os.path.dirname(APP))
            if rel_dir != ".":
                arc = "Payload/" + rel_dir.replace(os.sep, "/") + "/"
                info = zipfile.ZipInfo(arc)
                info.external_attr = (0o755 & 0xFFFF) << 16
                z.writestr(info, b"")
            for name in sorted(filenames):
                full = os.path.join(dirpath, name)
                rel = os.path.relpath(full, os.path.dirname(APP))
                arc = "Payload/" + rel.replace(os.sep, "/")
                mode = os.stat(full).st_mode
                # 可执行文件 0755，其余 0644。
                perm = 0o755 if (mode & stat.S_IXUSR) else 0o644
                info = zipfile.ZipInfo(arc)
                info.external_attr = (perm & 0xFFFF) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                with open(full, "rb") as f:
                    z.writestr(info, f.read())
                count += 1
    print(f"pack_ipa 完成：{IPA}（{count} 个文件）")
    with zipfile.ZipFile(IPA) as z:
        names = z.namelist()
        print("前 5 项：", names[:5])
        assert names[0].startswith("Payload/WB2Gateway.app/"), "ipa 首项不是 Payload/WB2Gateway.app/"


if __name__ == "__main__":
    main()
