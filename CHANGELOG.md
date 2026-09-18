# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-09-18

Initial release.

### Added

- `auth`, `list`, `show`, and `get` commands for the Concept2 Logbook API.
- TCX export with one `<Lap>` per Concept2 interval/split, densely sampled
  trackpoints, heart rate, cadence, and estimated watts (Concept2's power
  formula applied to RowErg/SkiErg/dynamic and BikeErg alike).
- `strava-auth` and `strava-upload` stretch commands for uploading exported
  `.tcx` files straight to Strava.
