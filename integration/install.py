#!/usr/bin/env python3
"""Install a real published DEB in a disposable Ubuntu container.

Download assets first with install.sh --download-only DIR (and --github-auth
while the repository is private). The curl fixture copies these verified release
files, so this tests real apt installation without copying credentials to Docker.
"""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("--packages", type=Path, required=True)
parser.add_argument("--tag", required=True)
args = parser.parse_args()
repo = Path(__file__).resolve().parents[1]
root = Path(tempfile.mkdtemp(prefix="gosync-install-validation-"))
packages = args.packages.resolve()
assert list(packages.glob("*.deb")), "Download the native Ubuntu package first"
(root / "Dockerfile").write_text("""FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl python3 procps shellcheck
COPY curl /fixture/curl
RUN chmod 755 /fixture/curl
""")
(root / "curl").write_text("""#!/usr/bin/env python3
import pathlib, shutil, sys
args=sys.argv[1:]
assert args[-1].startswith('https://github.com/TypeTerrors/go_sync/releases/download/')
name=args[-1].split('/')[-1]
assert name.startswith('gosync-desktop-linux-') and '/' not in name
shutil.copyfile(pathlib.Path('/packages')/name, args[args.index('-o')+1])
""")
subprocess.run(["docker", "build", "-t", "gosync-installer-test:local", str(root)], check=True)
command = ["docker", "run", "--rm", "--mount", f"type=bind,src={repo},dst=/source,readonly",
           "--mount", f"type=bind,src={packages},dst=/packages,readonly", "gosync-installer-test:local",
           "bash", "-c", """set -eu
shellcheck /source/install.sh
export PATH=/fixture:$PATH
bash /source/install.sh --no-launch --version "$1"
test "$(dpkg-query -W -f='${Status}' gosync-desktop)" = 'install ok installed'
gosync --help
test ! -e /root/.config/gosync/default.yaml
test ! -e /root/.config/systemd/user/gosync.service
python3 -c 'import gi; gi.require_version("Gtk", "3.0"); gi.require_foreign("cairo")'
echo 'PASS: native Ubuntu package, GTK dependencies, CLI and no service activation'
""", "installer-test", args.tag]
result = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
(root / "install.log").write_text(result.stdout)
lines = [line for line in result.stdout.splitlines() if line.startswith("  ") and any(
    word in line for word in ["Detected ", "Release v_", "Downloading gosync-", "SHA-256", "Installing the desktop",
                             "Installed GoSync", "Next: open GoSync", "Synchronization starts"])]
trace = {"platform": "Ubuntu 24.04 container", "release": args.tag,
         "command": "bash install.sh --no-launch --version " + args.tag,
         "download": "Published package downloaded with authenticated GitHub access, then mounted read-only; no credentials in container",
         "exit_code": result.returncode, "installer_output": lines}
(root / "trace.json").write_text(json.dumps(trace, indent=2) + "\n")
print(result.stdout[-5000:])
print("Evidence:", root)
if result.returncode:
    raise SystemExit(result.returncode)
