#!/usr/bin/env python3
"""patch_platform.py — 把 Go c-archive（GOOS=darwin 产物）里每个 Mach-O 目标文件的
LC_BUILD_VERSION 由 platform=1（macOS）改成 platform=2（iOS）、minos=15.0。

只改原地 8 个字节（platform、minos 各 4 字节），不改任何长度，.a 档案的
成员偏移与符号表全部保持有效。没有 LC_BUILD_VERSION 的成员跳过并报告；
遇到 LC_VERSION_MIN_MACOSX（老式命令，无 platform 字段）则报错退出，
因为它无法原地改成 iOS。

用法：python3 scripts/patch_platform.py out/libwbgateway.a
"""
import struct
import sys

LC_BUILD_VERSION = 0x32
LC_VERSION_MIN_MACOSX = 0x24
PLATFORM_MACOS = 1
PLATFORM_IOS = 2
MH_MAGIC_64 = 0xFEEDFACF
MH_CIGAM_64 = 0xCFFAEDFE
CPU_TYPE_ARM64 = 0x0100000C


def parse_macho(data, off, name):
    """解析一个 Mach-O 64 对象，返回 (platform, minos, sdk, ncmds) 或 None。"""
    if len(data) - off < 32:
        return None
    magic = struct.unpack_from("<I", data, off)[0]
    if magic == MH_CIGAM_64:
        endian = ">"
    elif magic == MH_MAGIC_64:
        endian = "<"
    else:
        return None
    (cputype, _, _, ncmds, sizeofcmds, _, _) = struct.unpack_from(endian + "IiiIIII", data, off + 4)
    p = off + 32
    build = None
    for _ in range(ncmds):
        if p + 8 > len(data):
            break
        cmd, cmdsize = struct.unpack_from(endian + "II", data, p)
        if cmd == LC_BUILD_VERSION and cmdsize >= 24:
            platform, minos, sdk, _ = struct.unpack_from(endian + "IIII", data, p + 8)
            build = (platform, minos, sdk, p + 8)
        elif cmd == LC_VERSION_MIN_MACOSX:
            raise SystemExit(f"{name}: 存在 LC_VERSION_MIN_MACOSX（老式命令），无法原地改成 iOS")
        p += cmdsize if cmdsize >= 8 else 8
    return (cputype, build)


def patch_archive(path):
    with open(path, "r+b") as f:
        data = bytearray(f.read())
    if data[:8] != b"!<arch>\n":
        # 不是 .a，可能是单个 Mach-O：直接处理。
        res = parse_macho(data, 0, path)
        if res is None:
            raise SystemExit(f"{path}: 不是 Mach-O 或 .a")
        cputype, build = res
        if build is None:
            raise SystemExit(f"{path}: 没有 LC_BUILD_VERSION")
        platform, minos, sdk, pos = build
        struct.pack_into("<I", data, pos, PLATFORM_IOS)
        struct.pack_into("<I", data, pos + 4, 0x000F0000)  # minos 15.0.0
        with open(path, "wb") as f:
            f.write(data)
        print(f"{path}: platform {platform}->2, minos {minos:#x}->15.0")
        return

    # ar 档案：遍历成员。
    pos = 8
    patched = 0
    checked = 0
    long_names = b""
    while pos + 60 <= len(data):
        header = bytes(data[pos:pos + 60])
        if header[:2] == b"`\n" or header[:8] == b"!<thin>\n":
            break
        name = header[0:16].decode("ascii", "replace").strip()
        try:
            size = int(header[48:58].decode("ascii").strip())
        except ValueError:
            break
        body_off = pos + 60
        body = data[body_off:body_off + size]
        if name in ("__.SYMDEF", "/", "//", "__.SYMDEF SORTED"):
            if name == "//":
                long_names = body
        else:
            # 长文件名成员：名字以 "/<offset>" 形式存在 "//" 表里。
            member_name = name
            if name.startswith("/") and name[1:].strip().isdigit():
                off = int(name[1:].strip())
                end = long_names.find(b"\n", off)
                member_name = long_names[off:end].decode("utf-8", "replace").strip()
            res = parse_macho(body, 0, member_name)
            if res is not None:
                cputype, build = res
                checked += 1
                if build is not None:
                    platform, minos, sdk, bpos = build
                    if platform != PLATFORM_MACOS:
                        print(f"  {member_name}: platform 已是 {platform}，跳过")
                        continue
                    struct.pack_into("<I", data, body_off + bpos, PLATFORM_IOS)
                    struct.pack_into("<I", data, body_off + bpos + 4, 0x000F0000)
                    patched += 1
                    print(f"  {member_name}: platform 1->2, minos {minos:#x}->15.0")
        pos = body_off + size + (size % 2)
    print(f"共检查 {checked} 个 Mach-O 成员，改了 {patched} 个 LC_BUILD_VERSION")
    if patched == 0:
        raise SystemExit("没有改到任何 LC_BUILD_VERSION，patch 失败")
    with open(path, "wb") as f:
        f.write(data)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("用法：python3 scripts/patch_platform.py <libwbgateway.a>")
    patch_archive(sys.argv[1])
