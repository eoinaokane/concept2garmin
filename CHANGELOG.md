# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-09-18

### Added

- `--version` flag reporting the CLI's semantic version.
- Automated release pipeline: a GitHub Actions workflow tags trigger
  GoReleaser, which cross-compiles binaries for macOS/Linux (amd64/arm64)
  and publishes a Homebrew cask to `eoinaokane/homebrew-tap`, so
  `brew install --cask eoinaokane/tap/concept2garmin` works without a local
  Go toolchain.
- README: a Prerequisites section (ErgData app/phone pairing, Concept2
  Logbook sync) and step-by-step instructions for obtaining a Concept2
  Logbook API token.

## [0.1.0] - 2026-09-18

Initial release.

### Added

- `auth`, `list`, `show`, and `get` commands for the Concept2 Logbook API.
- TCX export with one `<Lap>` per Concept2 interval/split, densely sampled
  trackpoints, heart rate, cadence, and estimated watts (Concept2's power
  formula applied to RowErg/SkiErg/dynamic and BikeErg alike).
- `strava-auth` and `strava-upload` stretch commands for uploading exported
  `.tcx` files straight to Strava.
