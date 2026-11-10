#!/usr/bin/env python3
"""gen_icons.py — 从 assets/icon-any.png 生成旧式 CFBundleIconFiles 尺寸。

没有 actool（Linux 上没有），故用旧式 CFBundleIconFiles + 对应尺寸 PNG
（iOS 27 侧载仍有效，用户已实测）。暗色 / 染色变体需要 asset catalog，
旧式方式不支持，只用 icon-any.png（通用外观）生成全部尺寸。

用法：python3 scripts/gen_icons.py [输出目录，默认 out/WB2Gateway.app]

尺寸（iPhone，pt×scale）：
  60pt @2x/@3x（主图标）、40pt @2x/@3x（Spotlight）、
  29pt @2x/@3x（设置）、20pt @2x/@3x（通知）、1024（App Store）。
文件名按 Xcode 旧式约定：AppIcon60x60@2x.png 等；
Info.plist 的 CFBundleIconFiles 只写基名（AppIcon60x60），
系统按 @2x/@3x 后缀自动挑。
"""
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SRC = ROOT / "assets" / "icon-any.png"

# (文件名, 边长 px)
SIZES = [
    ("AppIcon60x60@2x.png", 120),
    ("AppIcon60x60@3x.png", 180),
    ("AppIcon40x40@2x.png", 80),
    ("AppIcon40x40@3x.png", 120),
    ("AppIcon29x29@2x.png", 58),
    ("AppIcon29x29@3x.png", 87),
    ("AppIcon20x20@2x.png", 40),
    ("AppIcon20x20@3x.png", 60),
    ("AppIcon1024x1024.png", 1024),
]


def main():
    out_dir = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "out" / "WB2Gateway.app"
    from PIL import Image

    if not SRC.is_file():
        raise SystemExit(f"缺少图标源文件：{SRC}")
    img = Image.open(SRC)
    if img.size != (1024, 1024):
        print(f"注意：源图 {img.size} 不是 1024x1024，仍按比例缩放")
    # iOS 图标不带 alpha：源图是 RGB，直接保持；若带 alpha 则转 RGB（白底）。
    if img.mode not in ("RGB",):
        img = img.convert("RGB")
    out_dir.mkdir(parents=True, exist_ok=True)
    for name, size in SIZES:
        if size == 1024:
            # 1024 原尺寸：直接复制源文件（不重编码，避免质量损失）。
            import shutil
            shutil.copyfile(SRC, out_dir / name)
        else:
            im = img.resize((size, size), Image.LANCZOS)
            im.save(out_dir / name, "PNG", optimize=True)
        print(f"  {name} ({size}x{size})")
    print(f"gen_icons 完成：{len(SIZES)} 个 PNG → {out_dir}")


if __name__ == "__main__":
    main()
