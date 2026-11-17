# Encorr

**Request the encore.**

Encorr is a media request app for Plex. Your friends and family ask for a film or show, and it arrives in Plex without you lifting a finger. It talks to the Radarr and Sonarr you already run.

- One program file. No Docker, no Node, no Java.
- Runs on Windows and Linux (Mac later).
- Sets itself up: sign in with Plex, and it finds Radarr and Sonarr on the same machine for you.
- Dark frosted-glass look, with a violet or coral accent.

<p align="center"><a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest/download/Encorr-Windows.zip"><b>⬇ Download Encorr for Windows</b></a></p>

> This is an early test build. See [TESTING.md](TESTING.md) for how to try it on your server.

## Quick start (Windows)

1. [Download Encorr-Windows.zip](https://github.com/FiTH-g1gGLe/Encorr/releases/latest/download/Encorr-Windows.zip), unzip it and put `encorr.exe` somewhere permanent, e.g. `C:\Encorr\`.
2. Double-click it. Windows may warn that it's from an unknown publisher: choose **More info → Run anyway** (the build isn't code-signed yet). If the firewall asks, allow **private networks**.
3. Your browser opens at <http://localhost:5150>. Follow the setup wizard.

Encorr keeps its data in `C:\ProgramData\Encorr` (Linux: `~/.config/encorr`). Close the window, or press Ctrl+C, to stop it.

Options:

| Option | What it does |
| --- | --- |
| `-port 5150` | Port for the web page (default 5150) |
| `-config <folder>` | Where Encorr keeps its data |
| `-no-browser` | Don't open the browser on start |
| `-version` | Print the version |

## What works in 0.1.0

- **Setup wizard**: Plex sign-in, server and library picker, TMDB key, Radarr and Sonarr (auto-detected from their `config.xml`, with a Test button that fills every dropdown), Discord.
- **Discover and search**: trending, popular films and TV, coming soon to UK cinemas, search. Posters show what's already in Plex or requested.
- **Title pages**: details, trailer, seasons with their status, live download progress, "more like this".
- **Requests**: films and TV seasons, notes, approve/decline/retry, cancel your own, quotas for Friends.
- **Three roles**: Admin, Trusted (approved automatically), Friend (needs approval).
- **Sending to Radarr/Sonarr**: handles titles that are already there (unmonitored, monitored, downloaded), monitors only the requested seasons, tags each request with the requester's name.
- **Spotting downloads in Plex**: a light "recently added" check every 5 minutes, plus a full scan nightly at 3am.
- **Plex watchlists**: each user can turn on "request from my watchlist".
- **Discord**: rich messages for new requests, approvals, failures and "Your encore is ready", with optional @mentions.
- **Health page**: plain-English checks for Plex, TMDB, Radarr, Sonarr (including free space), Discord and the data folder.
- **Safety**: Plex sign-in only, secure session cookies, sign-in rate limiting, a CSRF guard, and API keys and tokens encrypted on disk.
- **Phone**: works in the browser; "Add to Home screen" gives an app icon.

## Not in 0.1.0 yet

- Windows installer, background service and tray icon, and the safe self-updater (build phase 4).
- Native Android app, Discord bot, invite links and the other "Next" features in the project plan.
- Svelte and SQLite: this build uses plain HTML/JavaScript and a single JSON data file, because packages couldn't be downloaded where it was built. Swapping them in later doesn't change how Encorr works.

## Building from source

Needs Go 1.24 or newer.

```sh
go build -o encorr ./cmd/encorr                                    # this machine
GOOS=windows GOARCH=amd64 go build -o encorr.exe ./cmd/encorr      # Windows
go test ./...                                                       # tests
```

### Trying it without a real server

`encorr-fake` pretends to be Plex, TMDB, Radarr, Sonarr and Discord:

```sh
go run ./cmd/encorr-fake -port 5199 &
ENCORR_DEV_FAKE=http://localhost:5199 go run ./cmd/encorr -config /tmp/encorr-dev
```

Use TMDB key `good-key`. For Radarr use address `http://localhost:5199/radarr` and key `radarr-key` (Sonarr: `/sonarr` and `sonarr-key`).

## Project layout

| Folder | What's in it |
| --- | --- |
| `cmd/encorr` | The program: starts the web server and background jobs |
| `internal/app` | Request engine, background jobs, health checks and the `/api/v1` API |
| `internal/arr` | Radarr and Sonarr client, including auto-detection |
| `internal/plex` | plex.tv sign-in, servers, friends, watchlists; library scans |
| `internal/tmdb` | Film and TV information from TMDB |
| `internal/notify` | Discord notifications |
| `internal/store` | The data file, with encrypted secrets and backups |
| `internal/web` | The web interface (built into the program) |
| `internal/fake` | Pretend services for tests and previews |

## Credits

Encorr's request flow follows ideas proven in [Seerr](https://github.com/seerr-team/seerr) (MIT), with ideas from Ombi, Watchlistarr, Requestrr, Doplarr, Reiverr and MediaManager. No code from GPL or AGPL projects is included. Film and TV data comes from [TMDB](https://www.themoviedb.org); Encorr uses the TMDB API but is not endorsed or certified by TMDB.

## Licence

MIT. See [LICENSE](LICENSE).
