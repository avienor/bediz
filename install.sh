#!/bin/sh
# Installs Bediz from a GitHub Release on Linux amd64 and macOS arm64:
#
#   curl -fsSL https://raw.githubusercontent.com/avienor/bediz/master/install.sh | sh
#
# It installs the latest release, or the tag in BEDIZ_VERSION, verifies the
# archive against the release's SHA256SUMS, and copies bediz to
# BEDIZ_INSTALL_DIR (default ~/.local/bin). It then installs the agent skill
# from the same tag for the user with npx when Node.js is available. It never
# edits shell profiles or uses sudo.
set -eu

repo=avienor/bediz

say() { printf '%s\n' "$*" >&2; }
fail() { say "bediz install: $*"; exit 1; }

platform() {
	os=$(uname -s)
	arch=$(uname -m)
	# A shell running under Rosetta reports x86_64 on Apple silicon.
	if [ "$os" = Darwin ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		arch=arm64
	fi
	case "$os/$arch" in
	Linux/x86_64 | Linux/amd64) echo linux_amd64 ;;
	Darwin/arm64) echo darwin_arm64 ;;
	*) fail "no release for $os $arch; releases exist for Linux amd64, macOS arm64 (Apple silicon), and Windows amd64" ;;
	esac
}

latest_tag() {
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "could not reach https://github.com/$repo/releases/latest"
	case "$url" in
	*/releases/tag/*) echo "${url##*/}" ;;
	*) fail "no stable release is published yet; choose a tag from https://github.com/$repo/releases and set BEDIZ_VERSION" ;;
	esac
}

verify() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum -c -
	else
		shasum -a 256 -c -
	fi
}

main() {
	command -v curl >/dev/null 2>&1 || fail "curl is required"
	platform=$(platform)
	tag=${BEDIZ_VERSION:-$(latest_tag)}
	dir=${BEDIZ_INSTALL_DIR:-$HOME/.local/bin}
	case "$dir" in
	/*) ;;
	*) dir="$PWD/$dir" ;;
	esac
	archive="bediz_${tag}_${platform}.tar.gz"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	cd "$tmp"

	say "Downloading Bediz $tag for $platform"
	base="https://github.com/$repo/releases/download/$tag"
	curl -fsSLO "$base/$archive" || fail "could not download $base/$archive"
	curl -fsSLO "$base/SHA256SUMS" || fail "could not download $base/SHA256SUMS"
	grep "  $archive\$" SHA256SUMS >archive.sum || fail "SHA256SUMS has no line for $archive; nothing was installed"
	verify <archive.sum >/dev/null || fail "the checksum of $archive did not verify; nothing was installed"

	tar -xzf "$archive" bediz
	mkdir -p "$dir"
	cp bediz "$dir/.bediz.new"
	chmod 755 "$dir/.bediz.new"
	mv -f "$dir/.bediz.new" "$dir/bediz"
	"$dir/bediz" version --json | grep -q "\"version\":\"$tag\"" ||
		fail "$dir/bediz does not report version $tag"
	say "Installed $dir/bediz"

	case ":$PATH:" in
	*":$dir:"*)
		found=$(command -v bediz || true)
		if [ "$found" != "$dir/bediz" ]; then
			say "Note: bediz resolves to $found, which comes before $dir on PATH"
		fi
		;;
	*) say "Note: $dir is not on PATH; add it to your shell profile, for example: export PATH=\"$dir:\$PATH\"" ;;
	esac

	skill="https://github.com/$repo/tree/$tag/skills/bediz"
	if command -v npx >/dev/null 2>&1; then
		say "Installing the Bediz agent skill from $tag"
		if npx -y skills add "$skill" -g -y; then
			say "Installed the agent skill; it loads in a new agent session"
		else
			say "Note: the agent skill was not installed; retry with: npx skills add $skill -g"
		fi
	else
		say "Note: the agent skill needs Node.js; after installing it, run: npx skills add $skill -g"
	fi
}

main "$@"
