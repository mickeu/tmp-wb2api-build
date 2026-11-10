#!/usr/bin/env python3
"""check_macho.py — 解析 .a 或单个 Mach-O，输出每个目标文件的
cputype、LC_BUILD_VERSION 的 platform/minos/sdk，供 verify.sh 的 V3/V4 用。

用法：
  python3 scripts/check_macho.py <file> [--json]
退出码：0 = 至少一个 Mach-O 含 LC_BUILD_VERSION 且全部满足
platform==2、minos==15.0、cputype==0x0100000c；否则 1。
"""
import json
import struct
import sys

LC_BUILD_VERSION = 0x32
MH_MAGIC_64 = 0xFEEDFACF
MH_CIGAM_64 = 0xCFFAEDFE
CPU_TYPE_ARM64 = 0x0100000C


def parse_macho(data, off):
    if len(data) - off < 32:
        return None
    magic = struct.unpack_from("<I", data, off)[0]
    if magic == MH_CIGAM_64:
        endian = ">"
    elif magic == MH_MAGIC_64:
        endian = "<"
    else:
        return None
    (cputype, _, _, ncmds, _, _, _) = struct.unpack_from(endian + "IiiIIII", data, off + 4)
    p = off + 32
    build = None
    for _ in range(ncmds):
        if p + 8 > len(data):
            break
        cmd, cmdsize = struct.unpack_from(endian + "II", data, p)
        if cmd == LC_BUILD_VERSION and cmdsize >= 24:
            platform, minos, sdk, _ = struct.unpack_from(endian + "IIII", data, p + 8)
            build = {"platform": platform, "minos": minos, "sdk": sdk}
        p += cmdsize if cmdsize >= 8 else 8
    return {"cputype": cputype, "build_version": build}


def fmt_ver(v):
    return "%d.%d.%d" % ((v >> 16) & 0xFFFF, (v >> 8) & 0xFF, v & 0xFF)


def scan(path):
    with open(path, "rb") as f:
        data = f.read()
    out = []
    if data[:8] == b"!<arch>\n":
        pos = 8
        long_names = b""
        while pos + 60 <= len(data):
            header = data[pos:pos + 60]
            name = header[0:16].decode("ascii", "replace").strip()
            try:
                size = int(header[48:58].decode("ascii").strip())
            except ValueError:
                break
            body_off = pos + 60
            body = data[body_off:body_off + size]
            if name == "//":
                long_names = body
            elif name not in ("__.SYMDEF", "/", "__.SYMDEF SORTED"):
                member_name = name
                if name.startswith("/") and name[1:].strip().isdigit():
                    off = int(name[1:].strip())
                    end = long_names.find(b"\n", off)
                    member_name = long_names[off:end].decode("utf-8", "replace").strip()
                r = parse_macho(body, 0)
                if r is not None:
                    r["member"] = member_name
                    out.append(r)
            pos = body_off + size + (size % 2)
    else:
        r = parse_macho(data, 0)
        if r is not None:
            r["member"] = path
            out.append(r)
    return out


def main():
    path = sys.argv[1]
    as_json = "--json" in sys.argv[1:]
    objs = scan(path)
    ok = False
    n_build = 0
    for o in objs:
        b = o["build_version"]
        if b is not None:
            n_build += 1
            o["ok"] = (b["platform"] == 2 and b["minos"] == 0x000F0000
                       and o["cputype"] == CPU_TYPE_ARM64)
        else:
            o["ok"] = None
    if n_build > 0:
        ok = all(o["ok"] for o in objs if o["build_version"] is not None)
    if as_json:
        print(json.dumps({"file": path, "objects": len(objs),
                          "with_build_version": n_build, "ok": ok,
                          "details": [{**o,
                                      "minos_str": fmt_ver(o["build_version"]["minos"]) if o["build_version"] else None}
                                     for o in objs]}, indent=1, ensure_ascii=False))
    else:
        for o in objs:
            b = o["build_version"]
            if b is None:
                print(f'{o["member"]}: cputype={o["cputype"]:#x} 无 LC_BUILD_VERSION')
            else:
                print(f'{o["member"]}: cputype={o["cputype"]:#x} '
                      f'platform={b["platform"]} minos={fmt_ver(b["minos"])} '
                      f'sdk={fmt_ver(b["sdk"])} ok={o["ok"]}')
        print(f"共 {len(objs)} 个 Mach-O，{n_build} 个含 LC_BUILD_VERSION，ok={ok}")
    return 0 if ok else 1


if __name__ == "__main__":
    if len(sys.argv) < 2:
        raise SystemExit("用法：python3 scripts/check_macho.py <file> [--json]")
    sys.exit(main())
