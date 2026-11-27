#!/bin/sh
set -eu

TARGETS="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"
MODULE="github.com/Spanless/AgyTheme"
SLUG="agy-theme"
PRODUCT="AgyTheme"

fail() {
  echo "[release] $*" >&2
  exit 1
}

env_value() {
  eval "current=\${$1:-}"
  if [ -n "$current" ]; then
    printf '%s' "$current"
  elif [ -f .env ]; then
    sed -n "s/^$1=//p" .env | tail -n 1 | tr -d '\r'
  fi
}

sign_tmp=""
cleanup() {
  [ -z "$sign_tmp" ] || rm -rf "$sign_tmp"
}
trap cleanup EXIT INT TERM

abs_dir() {
  case "$1" in
    "~") printf '%s' "$HOME" ;;
    "~/"*) printf '%s/%s' "$HOME" "${1#\~/}" ;;
    /*) printf '%s' "$1" ;;
    *) printf '%s/%s' "$PWD" "$1" ;;
  esac
}

sign_windows() {
  sign_tmp="$(mktemp -d)"
  chmod 700 "$sign_tmp"
  printf '%s' "$sign_pfx_b64" | base64 -d > "$sign_tmp/sign.pfx" 2>/dev/null || fail "WINDOWS_SIGN_PFX_BASE64 is not valid base64"
  printf '%s' "$sign_pfx_pass" > "$sign_tmp/pass"
  openssl pkcs12 -in "$sign_tmp/sign.pfx" -passin "file:$sign_tmp/pass" -nokeys -clcerts 2>/dev/null \
    | openssl x509 > "$sign_tmp/cert.pem" 2>/dev/null || fail "cannot open the PFX in .env (wrong WINDOWS_SIGN_PFX_PASSWORD?)"
  echo "[release] signing Windows binaries as $(openssl x509 -in "$sign_tmp/cert.pem" -noout -subject | sed 's/^subject=//')"
  for exe in "$bins"/agytheme-windows-*.exe; do
    osslsigncode sign -pkcs12 "$sign_tmp/sign.pfx" -readpass "$sign_tmp/pass" -h sha256 \
      -n "AgyTheme" -i "https://${MODULE}" -ts "$sign_ts_url" \
      -in "$exe" -out "$exe.signed" >/dev/null || fail "signing $exe failed"
    mv "$exe.signed" "$exe"
    osslsigncode verify -in "$exe" >/dev/null 2>&1 \
      || osslsigncode verify -in "$exe" -CAfile "$sign_tmp/cert.pem" >/dev/null 2>&1 \
      || fail "signature check failed for $exe"
    echo "  signed ${exe##*/}"
  done
  rm -rf "$sign_tmp"
  sign_tmp=""
}

tag=""
dry_run=0
skip_tests=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    --skip-tests) skip_tests=1 ;;
    -*) fail "unknown option $arg" ;;
    *) [ -z "$tag" ] || fail "only one tag may be given"; tag="$arg" ;;
  esac
done
[ -n "$tag" ] || fail "usage: scripts/release.sh vX.Y.Z [--dry-run] [--skip-tests]"
echo "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || fail "tag must look like v1.2.3 (got $tag)"
version="${tag#v}"

cd "$(dirname "$0")/.."
for tool in go git zip; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
done

cache_root="$(env_value DEVELOP_CACHE_ROOT)"
artifact_root="$(env_value DEVELOP_ARTIFACT_ROOT)"
cache_root="$(abs_dir "${cache_root:-$HOME/develop/cache}")"
artifact_root="$(abs_dir "${artifact_root:-$HOME/develop/artifacts}")"
export GOMODCACHE="${GOMODCACHE:-$cache_root/shared/go-mod}"
export GOCACHE="${GOCACHE:-$cache_root/shared/go-build}"
if [ "$(git rev-parse --git-dir)" = "$(git rev-parse --git-common-dir)" ]; then
  checkout="main"
else
  checkout="wt-$(basename "$(git rev-parse --show-toplevel)" | tr 'A-Z_ ' 'a-z--')"
fi
out="$cache_root/workspaces/$SLUG/staging/$checkout/$tag"
bins="$out/bin"
archive="$artifact_root/$SLUG/$version"

src_version="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' assets.go)"
pkg_version="$(sed -n 's/^  "version": "\(.*\)",$/\1/p' package.json)"
[ "$src_version" = "$version" ] || fail "assets.go has Version = \"$src_version\"; bump it to \"$version\" and commit first"
[ "$pkg_version" = "$version" ] || fail "package.json has version \"$pkg_version\"; bump it to \"$version\" and commit first"

sign_pfx_b64="$(env_value WINDOWS_SIGN_PFX_BASE64)"
sign_pfx_pass="$(env_value WINDOWS_SIGN_PFX_PASSWORD)"
sign_ts_url="$(env_value WINDOWS_SIGN_TIMESTAMP_URL)"
[ -n "$sign_ts_url" ] || sign_ts_url="http://timestamp.digicert.com"
sign_enabled=0
if [ -n "$sign_pfx_b64" ] && [ -n "$sign_pfx_pass" ]; then
  sign_enabled=1
  for tool in osslsigncode openssl; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool is required to sign the Windows binaries (brew install $tool)"
  done
elif [ "$dry_run" = 0 ]; then
  fail "no Windows signing certificate: set WINDOWS_SIGN_PFX_BASE64 and WINDOWS_SIGN_PFX_PASSWORD in .env (see .env.example)"
else
  echo "[release] no Windows signing certificate in .env; the dry run leaves the .exe files unsigned"
fi

if [ "$dry_run" = 0 ]; then
  command -v gh >/dev/null 2>&1 || fail "gh is required to publish"
  gh auth token -h github.com >/dev/null 2>&1 || fail "gh is not logged in (run: gh auth login)"
  [ -z "$(git status --porcelain)" ] || fail "working tree is not clean"
  [ "$(git rev-parse --abbrev-ref HEAD)" = main ] || fail "release from the main branch"
  git fetch -q origin main --tags
  [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || fail "HEAD is not origin/main; push or pull first"
  if gh release view "$tag" >/dev/null 2>&1; then
    fail "release $tag already exists"
  fi
  [ ! -e "$archive" ] || fail "$archive already exists; a published version is never overwritten"
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    [ "$(git rev-parse "$tag^{commit}")" = "$(git rev-parse HEAD)" ] || fail "tag $tag exists but does not point at HEAD"
  fi
fi

if [ "$skip_tests" = 0 ]; then
  echo "[release] go vet / go test -race"
  go vet ./...
  go test -race ./...
fi

echo "[release] building $tag with $(go version | awk '{ print $3 }') into $out"
rm -rf "$out"
mkdir -p "$bins"
for target in $TARGETS; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [ "$os" = windows ] && ext=".exe"
  bin="$bins/agytheme-${os}-${arch}${ext}"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X ${MODULE}.Version=${version}" -o "$bin" ./cmd/agytheme
  echo "  ${bin##*/}"
done
[ "$sign_enabled" = 0 ] || sign_windows

plugin_stage="$out.plugin"
rm -rf "$plugin_stage"
go run -ldflags "-X ${MODULE}.Version=${version}" ./cmd/agytheme stage-plugin "$plugin_stage/base"
cp SKILL.md "$plugin_stage/base/SKILL.md"
if command -v agy >/dev/null 2>&1 || [ -x "$HOME/.local/bin/agy" ]; then
  agy_bin="$(command -v agy 2>/dev/null || echo "$HOME/.local/bin/agy")"
  "$agy_bin" plugin validate "$plugin_stage/base" --strict >/dev/null || fail "agy plugin validate rejected the plugin folder"
  echo "  agy plugin validate: ok"
fi

pack_plugin() {
  zip_name="${PRODUCT}-${version}-$1.zip"
  shift
  rm -rf "$plugin_stage/pack"
  mkdir -p "$plugin_stage/pack/agytheme/bin"
  cp -R "$plugin_stage/base/." "$plugin_stage/pack/agytheme/"
  for os in "$@"; do
    cp "$bins"/agytheme-"$os"-* "$plugin_stage/pack/agytheme/bin/"
  done
  chmod 755 "$plugin_stage"/pack/agytheme/bin/*
  (cd "$plugin_stage/pack" && zip -qrX "$out/${zip_name}" agytheme)
  echo "  ${zip_name} ($(du -h "$out/${zip_name}" | awk '{ print $1 }'))"
}

echo "[release] packaging plugin zips"
PLATFORMS="darwin-universal windows-universal linux-universal all-platforms"
pack_plugin darwin-universal darwin
pack_plugin windows-universal windows
pack_plugin linux-universal linux
pack_plugin all-platforms darwin windows linux
rm -rf "$plugin_stage"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}
(cd "$out" && sha256 "${PRODUCT}-${version}"-*.zip > SHA256SUMS)

host="$bins/agytheme-$(go env GOOS)-$(go env GOARCH)"
[ "$(go env GOOS)" = windows ] && host="${host}.exe"
if [ -x "$host" ]; then
  reported="$("$host" version)"
  case "$reported" in
    *"$version"*) echo "[release] ${host##*/} version -> $reported" ;;
    *) fail "${host##*/} reports \"$reported\", expected $version" ;;
  esac
  if [ "$skip_tests" = 0 ]; then
    command -v bun >/dev/null 2>&1 || fail "bun is required for the runtime tests (or pass --skip-tests)"
    echo "[release] bun typecheck / bun test"
    bun install --no-env-file --frozen-lockfile >/dev/null
    bun --no-env-file run typecheck
    AGYTHEME_BIN="$host" bun --no-env-file test
  fi
fi

cat "$out/SHA256SUMS"
if [ "$dry_run" = 1 ]; then
  echo "[release] dry run: the build is in $out; nothing was tagged, uploaded or archived"
  exit 0
fi

if ! git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  git tag -a "$tag" -m "AgyTheme $tag"
fi
git push -q origin "refs/tags/$tag"
gh release create "$tag" "$out/${PRODUCT}-${version}"-*.zip "$out/SHA256SUMS" --verify-tag --generate-notes --title "$tag"
echo "[release] published $(gh release view "$tag" --json url --jq .url)"

archive_stage="$artifact_root/$SLUG/.staging-$version-$$"
rm -rf "$archive_stage"
for pa in $PLATFORMS; do
  zip_name="${PRODUCT}-${version}-${pa}.zip"
  mkdir -p "$archive_stage/$pa"
  cp -p "$out/$zip_name" "$archive_stage/$pa/"
  grep "  ${zip_name}\$" "$out/SHA256SUMS" > "$archive_stage/$pa/SHA256SUMS"
done
mv "$archive_stage" "$archive" || fail "could not move the archive into $archive (left in $archive_stage)"
rm -rf "$out"
echo "[release] archived to $archive"
