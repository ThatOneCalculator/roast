# Changelog

## 1.3.1 (2026-11-05)

## What's Changed
* docs: tighten the README for the public release by @amckinley in https://github.com/bluesky-social/roast/pull/122
* docs: explain tap trust for Homebrew upgrades by @amckinley in https://github.com/bluesky-social/roast/pull/124


**Full Changelog**: https://github.com/bluesky-social/roast/compare/v1.3.0...v1.3.1

## 1.3.0 (2026-11-05)

First public release of Roast. The public repository starts from a single commit; releases v1.1.0 through v1.2.1 remain available on [GitHub Releases](https://github.com/bluesky-social/roast/releases).

### Bug Fixes

* The untracked-file secret denylist matches SSH private keys with suffixes (for example `id_ed25519_github`).
* Configuration templates containing control characters are rejected, and the install summary and upload confirmation escape template-provided values.
* Dependency updates.

## 1.2.1 and earlier

Earlier releases: see [GitHub Releases](https://github.com/bluesky-social/roast/releases).
