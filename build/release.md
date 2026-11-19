# Release verification

Release workflows verify toolchain and configuration downloads against
`build/checksums.txt`. The Nancy CI workflow uses the same check.

## Updating inputs

Update the download URL and checksum together in a reviewed change. Verify new
checksums against trusted publisher metadata; do not replace a checksum just
because a download fails verification. Configuration URLs in
`.github/release.env` must use a specific release, not `latest`.

## Verifying a release

Before publishing, the release job verifies each binary's build attestation
against the repository, workflow, commit and tag. It also publishes `SHA256SUMS`
for all six assets, with a signed attestation in `SHA256SUMS.sigstore.json`.

Download all six assets and both verification files into one directory. Use a
current GitHub CLI, and set the expected tag and full commit SHA from a trusted
source:

```sh
version=vX.Y.Z
commit=EXPECTED_COMMIT_SHA
gh attestation verify SHA256SUMS --repo bnb-chain/bsc \
  --bundle SHA256SUMS.sigstore.json \
  --signer-workflow bnb-chain/bsc/.github/workflows/release.yml \
  --source-ref "refs/tags/$version" --source-digest "$commit" &&
sha256sum --check SHA256SUMS
```

For a pre-release use `.github/workflows/pre-release.yml` and its `pre-*` tag.
On macOS, `shasum -a 256 --check SHA256SUMS` can replace `sha256sum --check`.

## Before publishing

Attestation jobs need `id-token: write` and `attestations: write`; the release
job also needs `contents: write`. Signing uses GitHub Actions OIDC, not a stored
signing key.

Run `bash build/release-test.sh` for local checks. Before the first production
release, run a controlled pre-release to verify builds, signing and permissions.
Attestations verify provenance and integrity, not that the code is vulnerability-free.
