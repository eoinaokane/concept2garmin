# concept2garmin

Version 0.2.1 ([Semantic Versioning](https://semver.org/); see
[CHANGELOG.md](CHANGELOG.md) for release notes).

A small Go CLI that talks to the [Concept2 Logbook API](https://log.concept2.com/developers/documentation/)
to list your recent ergometer workouts and download one at a time as a
Garmin-compatible `.tcx` file (heart rate, cadence, and — for
rower/SkiErg/dynamic pieces with stroke data — estimated watts).

Uploading to Strava is included as a stretch feature, using Strava's own
upload API and OAuth flow.

## Prerequisites

- A workout recorded on a Concept2 erg (RowErg, BikeErg, SkiErg, or
  Dynamic) using the **ErgData** app on your phone, paired to the machine
  over Bluetooth, with that workout synced to your online
  [Concept2 Logbook](https://log.concept2.com/). This tool reads from your
  Logbook account, not from the erg or the app directly, so the workout
  needs to have made it there first.
- A Concept2 Logbook API access token — see [Requirements](#requirements)
  below for how to get one.

## Install

### Homebrew (macOS/Linux)

```bash
brew install --cask eoinaokane/tap/concept2garmin
```

### From source

Requires Go 1.22+:

```bash
git clone https://github.com/eoinaokane/concept2garmin.git
cd concept2garmin
make build        # builds ./dist/concept2garmin
```

## Quick start

```bash
concept2garmin auth your-access-token   # one-time, see Requirements below
concept2garmin list                     # see your recent workouts, numbered
concept2garmin get 1                    # download workout #1 as a .tcx file
```

See [Usage](#usage) below for the rest of the commands.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history. Known gaps and
planned work are tracked as
[GitHub issues](https://github.com/eoinaokane/concept2garmin/issues).

## Requirements

A Concept2 Logbook API access token. For personal use (this tool talks to
your own account only), the simplest way to get one is a self-service
long-lived token rather than registering a full OAuth app:

1. Log in at [log.concept2.com](https://log.concept2.com/).
2. Go to **Edit Profile > Applications > Concept2 Logbook API
   integration**.
3. Generate a long-lived authorization token there and copy it.

(Registering an OAuth application — for apps serving multiple users — is
also documented on the [developer docs](https://log.concept2.com/developers/documentation/)
site, but isn't needed for this tool.)

## Usage

The examples below use `./dist/concept2garmin` (a from-source build). If
you installed via Homebrew, `concept2garmin` is already on your `PATH` —
drop the `./dist/` prefix.

Save your token once — it's cached at `~/.config/concept2garmin/concept2.token`
(owner-only permissions) so you don't have to pass `--token` or set
`CONCEPT2_TOKEN` again:

```bash
./dist/concept2garmin auth your-access-token
```

`--token`/`CONCEPT2_TOKEN` still work and take priority when set (and are
themselves cached to that file for next time), so a one-off
`--token ...` on any command also works without running `auth` first.

List your 10 most recent workouts, numbered 1 (most recent) upward:

```bash
./dist/concept2garmin list
```

```
#   Date             Type     Distance        Time  Workout
1   2026-09-18 12:31 bike      13079m     30:00.0  VariableInterval
2   2026-09-07 06:56 bike        742m      2:23.8  JustRow
...
```

Show metadata for a single workout (by the position shown above) — date,
type, distance, duration, calories, heart rate, etc.:

```bash
./dist/concept2garmin show 1
```

Download a single workout (by the position shown above) as a `.tcx` file
into `./workout/`:

```bash
./dist/concept2garmin get 1
```

`get` only ever downloads one workout per invocation. It reuses the exact
result shown by your last `list` call when possible (via
`workout/.last_list.json`), so `get 3` really is the workout you saw at
position 3. It never overwrites an existing file — if the target name is
already taken, it appends `-1`, `-2`, etc.

Import the resulting `.tcx` file into Garmin Connect via
**Import Data** on the Garmin Connect website, or drag-and-drop it onto
Strava's **Upload Activity** page. Each Concept2 interval/split becomes its
own `<Lap>` (with per-lap average/max heart rate and cadence), and the file
carries a `<Notes>` crediting this project (plus your Concept2 comment, if
you left one on the workout).

### Stretch: uploading straight to Strava

1. Create a Strava API application at <https://www.strava.com/settings/api>
   and note its Client ID and Client Secret.
2. Authorize once (opens your browser; you log in and grant access
   yourself — this tool never sees your Strava password):
   ```bash
   export STRAVA_CLIENT_ID=...
   export STRAVA_CLIENT_SECRET=...
   ./dist/concept2garmin strava-auth
   ```
3. Upload everything in `./workout/` that hasn't been uploaded yet:
   ```bash
   ./dist/concept2garmin strava-upload
   ```

## How watts are computed

Concept2's published power formula, `watts = 2.80 / (split_seconds / 500)^3`,
is applied to every machine type. "Split" is whatever Concept2 itself uses
as the pace value: time per 500m for RowErg/SkiErg/dynamic, time per 1000m
for BikeErg — the same `/500` divisor is used either way, since Concept2
doesn't rescale BikeErg's split before applying the formula (see the
[Concept2 watts calculator](https://www.concept2.com/training/watts-calculator)
and [Erg Arcade's pace derivatives writeup](https://ergarcade.com/articles/c2-pace-derivatives)).

Workouts with stroke-by-stroke data (`stroke_data: true` in the API) get a
per-stroke watts value. Workouts without it fall back to one trackpoint per
interval/split, with watts estimated from each segment's own average pace.

## Development

```bash
make fmt-check vet build test
```
