#!/bin/bash
set -euo pipefail
repo_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_dir"
if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
  echo "当前发布脚本仅验收 macOS arm64。" >&2
  exit 1
fi
.tools/wails build -m -nosyncgomod
release_version="$(python3 -c 'import json; print(json.load(open("wails.json"))["info"]["productVersion"])')"
release_dir="$(mktemp -d "${TMPDIR:-/tmp}/tongxi-release.XXXXXX")"
trap 'rm -rf "$release_dir"' EXIT
release_image="$repo_dir/build/bin/Tongxi-${release_version}-macos-arm64.dmg"
ditto build/bin/tongxi.app "$release_dir/Tongxi.app"
python3 scripts/release-notices.py "$release_dir/Tongxi.app/Contents/Resources/ThirdPartyNotices.txt"
codesign --force --deep --sign - "$release_dir/Tongxi.app"
codesign --verify --deep --strict "$release_dir/Tongxi.app"
ln -s /Applications "$release_dir/Applications"
cp INSTALL.md "$release_dir/安装与使用.md"
cp THIRD_PARTY_NOTICES.md "$release_dir/第三方资源声明.md"
hdiutil create -ov -volname "Tongxi ${release_version}" -srcfolder "$release_dir" -format UDZO "$release_image"
hdiutil verify "$release_image"
(
  cd "$repo_dir/build/bin"
  shasum -a 256 "Tongxi-${release_version}-macos-arm64.dmg" > "Tongxi-${release_version}-macos-arm64.dmg.sha256"
)
echo "发布包：$release_image"
