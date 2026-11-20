#!/usr/bin/env bash
set -euo pipefail

source_directory=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
mkdir -p "$workspace/build" "$workspace/bin" "$workspace/output"
cp "$source_directory/download-verified.sh" "$workspace/build/"
printf 'verified input\n' > "$workspace/input"
checksum=$(sha256sum "$workspace/input" | awk '{ print $1 }')
printf '%s  input.tgz\n' "$checksum" > "$workspace/build/checksums.txt"

cat > "$workspace/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
while [[ $# -gt 0 ]]; do
    if [[ $1 == -o ]]; then
        cp "$DOWNLOAD_INPUT" "$2"
        if [[ ${DOWNLOAD_FAIL:-0} == 1 ]]; then
            exit 22
        fi
        exit 0
    fi
    shift
done
exit 1
CURL
chmod +x "$workspace/bin/curl"
export PATH="$workspace/bin:$PATH"
export DOWNLOAD_INPUT="$workspace/input"

expect_failure() {
    if bash "$workspace/build/download-verified.sh" "$@" > "$workspace/failure.log" 2>&1; then
        echo "Unexpected download success: $*" >&2
        exit 1
    fi
    if find "$workspace/output" -type f | grep -q .; then
        echo "Failed download left an output file" >&2
        exit 1
    fi
}

bash "$workspace/build/download-verified.sh" https://example.org/input.tgz "$workspace/output/input with spaces"
cmp "$workspace/input" "$workspace/output/input with spaces"
rm "$workspace/output/input with spaces"

printf 'tampered input\n' > "$workspace/input"
expect_failure https://example.org/input.tgz "$workspace/output/input"
printf 'previous verified input\n' > "$workspace/output/input"
cp "$workspace/output/input" "$workspace/previous"
if bash "$workspace/build/download-verified.sh" https://example.org/input.tgz "$workspace/output/input" > "$workspace/failure.log" 2>&1; then
    echo "Tampered download replaced an existing file" >&2
    exit 1
fi
cmp "$workspace/previous" "$workspace/output/input"
rm "$workspace/output/input"
expect_failure https://example.org/unknown.tgz "$workspace/output/input"
expect_failure http://example.org/input.tgz "$workspace/output/input"
expect_failure https://example.org/input.tgz ""
expect_failure https://example.org/input.tgz "$workspace/output"
expect_failure https://example.org/input.tgz
DOWNLOAD_FAIL=1 expect_failure https://example.org/input.tgz "$workspace/output/input"
mv "$workspace/build/checksums.txt" "$workspace/checksums.txt"
expect_failure https://example.org/input.tgz "$workspace/output/input"
mv "$workspace/checksums.txt" "$workspace/build/checksums.txt"
printf 'invalid  input.tgz\n' > "$workspace/build/checksums.txt"
expect_failure https://example.org/input.tgz "$workspace/output/input"
printf '%s  input.tgz\n' "$checksum" > "$workspace/build/checksums.txt"
printf '%s  input.tgz\n' "$checksum" >> "$workspace/build/checksums.txt"
expect_failure https://example.org/input.tgz "$workspace/output/input"

mkdir -p "$workspace/assets"/{linux,macos,windows,arm64} "$workspace/flat"
artifacts=(linux/geth_linux macos/geth_macos windows/geth_windows.exe arm64/geth-linux-arm64 mainnet.zip testnet.zip)
for artifact in "${artifacts[@]}"; do
    printf '%s\n' "$artifact" > "$workspace/assets/$artifact"
    cp "$workspace/assets/$artifact" "$workspace/flat/"
done
(
    cd "$workspace/assets"
    bash "$source_directory/release-checksums.sh" > "$workspace/flat/SHA256SUMS"
)
[[ $(wc -l < "$workspace/flat/SHA256SUMS") -eq 6 ]]
(
    cd "$workspace/flat"
    sha256sum --check SHA256SUMS
    for artifact in "${artifacts[@]}"; do
        filename=$(basename "$artifact")
        printf 'tampered asset\n' > "$filename"
        if sha256sum --check SHA256SUMS > "$workspace/failure.log" 2>&1; then
            echo "Tampered asset passed verification: $filename" >&2
            exit 1
        fi
        cp "$workspace/assets/$artifact" "$filename"
    done
)
for artifact in "${artifacts[@]}"; do
    mv "$workspace/assets/$artifact" "$workspace/missing"
    if (cd "$workspace/assets" && bash "$source_directory/release-checksums.sh" > "$workspace/failure.log" 2>&1); then
        echo "Missing asset was not rejected: $artifact" >&2
        exit 1
    fi
    mv "$workspace/missing" "$workspace/assets/$artifact"
done
echo "Release verification tests passed"
