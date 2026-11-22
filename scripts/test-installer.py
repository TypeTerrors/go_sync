#!/usr/bin/env python3
"""Installer contract tests. All installation commands are isolated fixtures."""
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "install.sh"
TAG = "v_abcdef.6b02a58f"

MOCK = r'''
import hashlib,json,os,pathlib,plistlib,shutil,subprocess,sys
name=pathlib.Path(sys.argv[0]).name; args=sys.argv[1:]; root=pathlib.Path(os.environ['GOSYNC_FIXTURE'])
with (root/'calls').open('a') as f:f.write(json.dumps([name]+args)+'\n')
cfg=json.loads((root/'settings.json').read_text())
if name=='uname': print(cfg['os'] if args==['-s'] else cfg['arch'])
elif name=='id': print(cfg.get('uid',1000))
elif name=='pgrep': sys.exit(0 if cfg.get('running') else 1)
elif name=='systemctl': print(cfg.get('service','inactive'))
elif name=='launchctl': sys.exit(0 if cfg.get('loaded') else 1)
elif name=='sw_vers': print(cfg.get('version','14.0'))
elif name=='sysctl': print(cfg.get('silicon',0))
elif name=='dpkg': print(cfg['arch'])
elif name=='dpkg-deb': print('gosync-desktop' if args[-1]=='Package' else cfg['arch'])
elif name in ['sudo','apt-get','open','nohup']: pass
elif name=='curl':
    target=pathlib.Path(args[args.index('-o')+1]);url=args[-1]
    if cfg.get('download_fail'):sys.exit(22)
    if 'api.github.com' in url:shutil.copyfile(root/'releases.json',target)
    elif url.endswith('.sha256'):
        asset=url.split('/')[-1][:-7]
        checksum='0'*64 if cfg.get('corrupt') else hashlib.sha256(b'fixture package').hexdigest()
        target.write_text(checksum+'  '+('wrong.zip' if cfg.get('bad_checksum') else asset)+'\n')
    else:target.write_bytes(b'fixture package')
elif name=='osascript':
    # JXA itself is exercised natively on the Mac CI runners below.
    rows=json.loads((root/'releases.json').read_text());asset=args[-1]
    candidates=[r['tag_name'] for r in rows if not r.get('draft') and not r.get('prerelease') and
        {asset,asset+'.sha256'} <= {a['name'] for a in r['assets']}]
    print(max(candidates,key=lambda tag:int(tag.split('.')[1],16)))
elif name=='unzip':print('GoSync.app/\nGoSync.app/Contents/Info.plist')
elif name=='ditto':
    if args[:2]==['-x','-k']:shutil.copytree(root/'bundle',pathlib.Path(args[-1])/'GoSync.app')
    else:shutil.copytree(args[0],args[1])
elif name=='plutil':
    with open(args[-1],'rb') as f:print(plistlib.load(f)['CFBundleIdentifier'])
elif name=='codesign':sys.exit(1 if cfg.get('bad_signature') else 0)
elif name=='mv':
    if cfg.get('fail_replace') and '/.gosync-install.' in args[0] and args[0].endswith('/GoSync.app'):
        sys.exit(1)
    sys.exit(subprocess.call(['/bin/mv']+args))
else:raise SystemExit('Unexpected mock command '+name)
'''


def release(tag, assets, **extra):
    return {"tag_name": tag, "draft": False, "prerelease": False,
            "assets": [{"name": name} for name in assets], **extra}


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="gosync-installer-test-")
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for command in ["uname", "id", "pgrep", "systemctl", "launchctl", "sw_vers", "sysctl", "dpkg",
                        "dpkg-deb", "sudo", "apt-get", "open", "nohup", "curl", "osascript", "unzip",
                        "ditto", "plutil", "codesign", "mv"]:
            file = self.bin / command
            file.write_text(f"#!{sys.executable}\n" + MOCK)
            file.chmod(0o755)
        self.cfg = {"os": "Linux", "arch": "amd64"}
        self.os_release = self.root / "os-release"
        self.os_release.write_text('ID=ubuntu\nVERSION_ID="24.04"\n')
        self.script = self.root / "install.sh"
        self.script.write_text(SCRIPT.read_text().replace('/etc/os-release', str(self.os_release)))
        self.assets = []
        for platform in ["linux", "darwin"]:
            for arch in ["amd64", "arm64"]:
                name = f"gosync-desktop-{platform}-{arch}." + ("deb" if platform == "linux" else "zip")
                self.assets += [name, name + ".sha256"]
        self.releases = [release(TAG, self.assets)]
        bundle = self.root / "bundle/Contents"
        (bundle / "MacOS").mkdir(parents=True)
        (bundle / "Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "com.typeterrors.gosync.desktop"}))
        for name in ["gosync", "gosync-desktop"]:
            p = bundle / "MacOS" / name
            p.write_text("fixture executable\n")
            p.chmod(0o755)

    def tearDown(self):
        self.temp.cleanup()

    def run_install(self, *args, success=True):
        (self.root / "settings.json").write_text(json.dumps(self.cfg))
        (self.root / "releases.json").write_text(json.dumps(self.releases))
        (self.root / "calls").write_text("")
        env = dict(os.environ, GOSYNC_FIXTURE=str(self.root), PATH=str(self.bin) + os.pathsep + os.environ['PATH'])
        result = subprocess.run(["bash", str(self.script), *args], env=env, text=True, encoding="utf-8", errors="backslashreplace", capture_output=True, timeout=30)
        self.calls = [json.loads(line) for line in (self.root / "calls").read_text().splitlines()]
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        # Every unique installer temp directory must be cleaned after success/failure.
        for command in self.calls:
            if command[0] == "curl":
                self.assertFalse(Path(command[command.index('-o') + 1]).parent.exists(), "download directory leaked")
        return result

    def no_install_commands(self):
        self.assertFalse(any(c[0] in ["sudo", "apt-get", "ditto", "mv", "open"] for c in self.calls), self.calls)

    def test_all_platform_architectures_download_verified_assets(self):
        for system, arch in [("Linux", "amd64"), ("Linux", "arm64"), ("Darwin", "x86_64"), ("Darwin", "arm64")]:
            with self.subTest(system=system, arch=arch):
                self.cfg.update(os=system, arch=arch)
                target = self.root / f"download-{system}-{arch}"
                self.run_install("--download-only", str(target))
                self.assertEqual(len(list(target.iterdir())), 2)
                self.no_install_commands()

    def test_rosetta_selects_apple_silicon(self):
        self.cfg.update(os="Darwin", arch="x86_64", silicon=1)
        self.run_install("--download-only", str(self.root / "download"))
        self.assertTrue((self.root / "download/gosync-desktop-darwin-arm64.zip").exists())

    def test_latest_ignores_order_badge_incomplete_and_prerelease(self):
        self.releases = [release("v_111111.6b02498c", ["gosync-linux-amd64"]),
                         release(TAG, self.assets), release("v_222222.6b02b000", self.assets, draft=True),
                         release("v_333333.6b02c000", self.assets, prerelease=True),
                         release("v_444444.6b02d000", ["gosync-desktop-linux-amd64.deb"]),
                         release("v_555555.6b020000", self.assets)]
        result = self.run_install("--download-only", str(self.root / "download"))
        self.assertIn("Release " + TAG, result.stdout)

    def test_pinned_version_skips_release_api(self):
        self.run_install("--version", TAG, "--download-only", str(self.root / "download"))
        self.assertFalse(any('api.github.com' in arg for c in self.calls for arg in c))

    def test_no_complete_release_is_failure(self):
        self.releases = [release(TAG, [])]
        self.run_install("--no-launch", success=False)
        self.no_install_commands()

    def test_checksum_failures_never_install(self):
        for flag in ["corrupt", "bad_checksum", "download_fail"]:
            with self.subTest(flag=flag):
                self.cfg[flag] = True
                self.run_install("--no-launch", success=False)
                self.no_install_commands()
                self.cfg.pop(flag)

    def test_running_process_or_service_prevents_download(self):
        for setting in [{"running": True}, {"service": "activating"}]:
            with self.subTest(setting=setting):
                self.cfg.update(setting)
                self.run_install("--no-launch", success=False)
                self.assertFalse(any(c[0] == "curl" for c in self.calls))
                self.cfg = {"os": "Linux", "arch": "amd64"}

    def test_loaded_launchagent_prevents_update(self):
        self.cfg.update(os="Darwin", arch="arm64", loaded=True)
        self.run_install("--no-launch", "--app-dir", str(self.root / "Apps"), success=False)
        self.no_install_commands()

    def test_ubuntu_install_uses_apt_and_never_starts_service(self):
        self.run_install("--no-launch")
        apt = [c for c in self.calls if c[:2] == ["sudo", "apt-get"]]
        self.assertEqual(len(apt), 2)
        self.assertIn("install", apt[-1])
        self.assertTrue(apt[-1][-1].endswith('gosync-desktop-linux-amd64.deb'))
        self.assertFalse(any(c[0] == 'nohup' for c in self.calls))
        self.assertFalse(any('start' in c for c in self.calls))

    def test_unsupported_platforms_and_options(self):
        for change in [{"os": "FreeBSD"}, {"arch": "armhf"}, {"os": "Darwin", "version": "11.7"}, {"uid": 0}]:
            with self.subTest(change=change):
                self.cfg = {"os": "Linux", "arch": "amd64", **change}
                self.run_install(success=False)
                self.no_install_commands()
        self.cfg = {"os": "Linux", "arch": "amd64"}
        for args in [("--version", "../bad"), ("--app-dir",), ("--unknown",)]:
            self.run_install(*args, success=False)
            self.no_install_commands()
        self.os_release.write_text('ID=ubuntu\nVERSION_ID="22.04"\n')
        self.run_install("--no-launch", success=False)
        self.no_install_commands()

    def test_existing_download_not_overwritten(self):
        target = self.root / "download"
        target.mkdir()
        existing = target / "gosync-desktop-linux-amd64.deb"
        existing.write_text("keep this")
        self.run_install("--download-only", str(target), success=False)
        self.assertEqual(existing.read_text(), "keep this")

    def test_mac_install_and_update_in_path_with_spaces(self):
        self.cfg.update(os="Darwin", arch="arm64")
        target = self.root / "My Applications"
        for _ in range(2):
            self.run_install("--no-launch", "--app-dir", str(target))
            self.assertTrue((target / "GoSync.app/Contents/MacOS/gosync").exists())
            self.assertEqual(list(target.iterdir()), [target / "GoSync.app"])

    def test_mac_replacement_failure_restores_old_app(self):
        self.cfg.update(os="Darwin", arch="arm64", fail_replace=True)
        target = self.root / "Apps"
        shutil.copytree(self.root / "bundle", target / "GoSync.app")
        old = target / "GoSync.app/keep-old-version"
        old.write_text("old")
        self.run_install("--no-launch", "--app-dir", str(target), success=False)
        self.assertEqual(old.read_text(), "old")
        self.assertEqual(list(target.iterdir()), [target / "GoSync.app"])

    def test_mac_signature_failure_preserves_existing_app(self):
        self.cfg.update(os="Darwin", arch="arm64", bad_signature=True)
        target = self.root / "Apps"
        shutil.copytree(self.root / "bundle", target / "GoSync.app")
        old = target / "GoSync.app/keep-old-version"
        old.write_text("old")
        self.run_install("--no-launch", "--app-dir", str(target), success=False)
        self.assertEqual(old.read_text(), "old")
        self.assertFalse(any(c[0] == "mv" for c in self.calls))

    @unittest.skipUnless(sys.platform == "darwin", "JXA is tested on native macOS runners")
    def test_native_jxa_selects_latest_complete_release(self):
        (self.root / "releases.json").write_text(json.dumps([
            release("v_111111.100", self.assets), release(TAG, self.assets),
            release("v_222222.ffffffff", self.assets, prerelease=True)]))
        source = SCRIPT.read_text().rsplit('main "$@"', 1)[0]
        # Run the exact embedded JXA through Apple's interpreter, without mocks.
        source += '\nplatform=darwin; work="$1"; asset=gosync-desktop-darwin-arm64.zip; select_release\n'
        result = subprocess.run(["bash", "-c", source, "test", str(self.root)], text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), TAG)


if __name__ == "__main__":
    unittest.main()
