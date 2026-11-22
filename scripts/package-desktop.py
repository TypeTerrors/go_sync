#!/usr/bin/env python3
"""Package already built native binaries. No installation or user-state changes."""
import pathlib
import json
import plistlib
import shutil
import subprocess
import sys

system, arch, staging = sys.argv[1:]
stage = pathlib.Path(staging)
commit = subprocess.check_output(["git", "rev-parse", "--short=12", "HEAD"], text=True).strip()
epoch = subprocess.check_output(["git", "show", "-s", "--format=%ct", "HEAD"], text=True).strip()


def copy_licenses(destination):
    """Ship upstream notices with the binaries, including transitive modules."""
    destination.mkdir(parents=True, exist_ok=True)
    output = subprocess.check_output(["go", "list", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    while output.strip():
        module, end = decoder.raw_decode(output.lstrip())
        output = output.lstrip()[end:]
        if not module.get("Dir") or module.get("Main"):
            continue
        root = pathlib.Path(module["Dir"])
        for pattern in ["LICENSE*", "COPYING*", "NOTICE*"]:
            for notice in root.glob(pattern):
                if notice.is_file():
                    name = module["Path"].replace("/", "_") + "-" + notice.name
                    shutil.copyfile(notice, destination / name)


if system == "linux":
    package = stage / "package"
    if package.exists():
        shutil.rmtree(package)
    for directory in ["DEBIAN", "usr/lib/gosync", "usr/bin", "usr/share/applications",
                      "usr/share/icons/hicolor/512x512/apps", "usr/share/doc/gosync-desktop"]:
        (package / directory).mkdir(parents=True, exist_ok=True)
    for name in ["gosync", "gosync-desktop"]:
        shutil.copy2(stage / "bin" / name, package / "usr/lib/gosync" / name)
        (package / "usr/bin" / name).symlink_to("../lib/gosync/" + name)
    shutil.copy2("deploy/linux/gosync-desktop.desktop", package / "usr/share/applications")
    shutil.copy2("assets/icons/gosync.png", package / "usr/share/icons/hicolor/512x512/apps")
    shutil.copy2("docs/DESKTOP.md", package / "usr/share/doc/gosync-desktop")
    shutil.copy2("docs/DESKTOP_DEPENDENCIES.md", package / "usr/share/doc/gosync-desktop")
    shutil.copy2("LICENSE", package / "usr/share/doc/gosync-desktop/copyright")
    shutil.copy2("docs/INSTALLING.md", package / "usr/share/doc/gosync-desktop")
    copy_licenses(package / "usr/share/doc/gosync-desktop/third-party")
    (package / "DEBIAN/control").write_text(f"""Package: gosync-desktop
Version: 0.1.0+git{epoch}.{commit}
Architecture: {arch}
Maintainer: TypeTerrors
Section: net
Priority: optional
Depends: libc6 (>= 2.34), zenity, xdg-utils, systemd, python3, python3-gi, python3-gi-cairo, gir1.2-gtk-3.0
Recommends: gnome-shell-extension-appindicator
Homepage: https://github.com/TypeTerrors/go_sync
Description: GoSync desktop tray and synchronization daemon
 Native desktop controls for a per-user GoSync background service.
 Synchronizes a selected folder with peers on the local network.
""")
    subprocess.run(["dpkg-deb", "--root-owner-group", "--build", str(package),
                    f"dist/gosync-desktop-linux-{arch}.deb"], check=True)
elif system == "darwin":
    app = stage / "GoSync.app"
    if app.exists():
        shutil.rmtree(app)
    macos, resources = app / "Contents/MacOS", app / "Contents/Resources"
    macos.mkdir(parents=True)
    resources.mkdir()
    for name in ["gosync", "gosync-desktop"]:
        shutil.copy2(stage / "bin" / name, macos / name)
    with open("deploy/macos/Info.plist", "rb") as source:
        info = plistlib.load(source)
    # Three numeric components, rather than a timestamp exceeding Apple's
    # CFBundleVersion component lengths. Exact source identity is separate.
    count = int(subprocess.check_output(["git", "rev-list", "--count", "HEAD"], text=True))
    info["CFBundleVersion"] = f"{1 + count // 10000}.{(count // 100) % 100}.{count % 100}"
    info["GoSyncRevision"] = commit
    with (app / "Contents/Info.plist").open("wb") as destination:
        plistlib.dump(info, destination)
    iconset = stage / "GoSync.iconset"
    iconset.mkdir(exist_ok=True)
    for size in [16, 32, 128, 256, 512]:
        for scale in [1, 2]:
            name = f"icon_{size}x{size}" + ("@2x" if scale == 2 else "") + ".png"
            subprocess.run(["sips", "-z", str(size * scale), str(size * scale),
                            "assets/icons/gosync.png", "--out", str(iconset / name)], check=True,
                           stdout=subprocess.DEVNULL)
    subprocess.run(["iconutil", "-c", "icns", str(iconset), "-o", str(resources / "GoSync.icns")], check=True)
    shutil.copy2("LICENSE", resources / "LICENSE")
    for doc in ["DESKTOP.md", "DESKTOP_DEPENDENCIES.md", "INSTALLING.md"]:
        shutil.copy2("docs/" + doc, resources)
    copy_licenses(resources / "third-party")
    # Ad-hoc signatures enable native execution; these are NOT Developer ID
    # signatures or notarization. Distribution signing is documented separately.
    for target in [macos / "gosync", macos / "gosync-desktop", app]:
        subprocess.run(["codesign", "--force", "--sign", "-", "--timestamp=none", str(target)], check=True)
    subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)
    subprocess.run(["plutil", "-lint", str(app / "Contents/Info.plist")], check=True)
    subprocess.run(["ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(app),
                    f"dist/gosync-desktop-darwin-{arch}.zip"], check=True)
else:
    raise SystemExit("Unsupported platform: " + system)
