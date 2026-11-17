<p align="center"><img src="docs/encorr-logo.svg" width="380" alt="Encorr"></p>

<p align="center"><b>Request the encore.</b><br>A friendly media request app for Plex. Friends ask for a film or show, and it turns up in Plex by itself.</p>

<p align="center">
  <a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/FiTH-g1gGLe/Encorr?label=release&color=9b7bff"></a>
  <a href="https://github.com/FiTH-g1gGLe/Encorr/actions/workflows/build.yml"><img alt="Build" src="https://img.shields.io/github/actions/workflow/status/FiTH-g1gGLe/Encorr/build.yml?branch=main&label=build"></a>
  <a href="https://github.com/FiTH-g1gGLe/Encorr/releases"><img alt="Downloads" src="https://img.shields.io/github/downloads/FiTH-g1gGLe/Encorr/total?color=34d399"></a>
  <a href="LICENSE"><img alt="Licence: MIT" src="https://img.shields.io/github/license/FiTH-g1gGLe/Encorr?color=60a5fa"></a>
  <img alt="Go" src="https://img.shields.io/github/go-mod/go-version/FiTH-g1gGLe/Encorr?logo=go&logoColor=white">
  <img alt="Windows and Linux" src="https://img.shields.io/badge/runs%20on-Windows%20%7C%20Linux-121a3f">
  <img alt="No Docker needed" src="https://img.shields.io/badge/Docker-not%20needed-121a3f">
  <img alt="Works with Plex, Radarr, Sonarr, SABnzbd" src="https://img.shields.io/badge/works%20with-Plex%20%C2%B7%20Radarr%20%C2%B7%20Sonarr%20%C2%B7%20SABnzbd-e5a00d">
</p>

<p align="center"><a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest/download/Encorr-Windows.zip"><b>⬇️ Download Encorr for Windows</b></a> &nbsp;·&nbsp; <a href="TESTING.md">🧪 Testing guide</a> &nbsp;·&nbsp; <a href="https://github.com/FiTH-g1gGLe/Encorr/releases">📝 What's new</a></p>

<p align="center"><img src="docs/screenshots/home.png" width="860" alt="Encorr home page: a greeting, a big search box, a three-step guide and trending films"></p>

## What is it? 🍿

Encorr is the front door to your Plex server. People sign in, find something to watch, and press **Request**. Encorr hands it to Radarr (films) or Sonarr (TV), SABnzbd downloads it, and Encorr tells them when it's ready to watch.

It does the same job as Overseerr, Jellyseerr and Ombi, but it's built to be:

- **🪶 Simple to run:** one program file. No Docker, no Node, no Java, no database server.
- **🧭 Simple to set up:** a wizard signs you in with Plex and finds Radarr, Sonarr and SABnzbd on the same machine by itself.
- **🙂 Simple to use:** plain English everywhere, big buttons, and a progress tracker anyone can read.
- **🌙 Always on:** one click runs it in the background as a Windows service that starts with the computer.

## Features ✨

| | |
| --- | --- |
| 🗺️ **Request journey** | Every request shows four steps: Requested → Approved → Downloading → In Plex. The line fills as it downloads, and the last step lights up when it's ready. |
| ⬇️ **Live downloads** | Percentage, speed and time left come straight from SABnzbd, refreshed every few seconds, with plain stages like "📦 Unpacking" and "🚚 Moving it into your library". |
| ⚡ **Instant "ready"** | When a download finishes, Encorr asks Plex to scan just that folder and checks within seconds, so requests turn green almost straight away. |
| 👥 **Accounts for friends** | Make accounts with a username and password, or let people sign in with Plex. Owner, Trusted (no approval needed) and Friend (needs approval, with weekly limits). |
| 🔎 **Discover and search** | Trending, popular films and TV, and coming soon to UK cinemas. Posters show what's already in Plex or already requested. |
| 📺 **Proper TV handling** | Pick seasons; only those are monitored. Part-arrived shows say "18 of 24 episodes in Plex". |
| 🔁 **Clever hand-off** | Handles titles Radarr/Sonarr already have (unmonitored, monitored or downloaded) without duplicates. |
| 👀 **Plex watchlists** | Add something to your Plex watchlist and Encorr requests it for you. |
| 💬 **Discord** | Rich messages for new requests, approvals, failures and "Your encore is ready", with @mentions. |
| ❤️ **Health page** | Plain-English checks for Plex, TMDB, Radarr, Sonarr (including free disk space), SABnzbd, Discord and the data folder. |
| 🌍 **Reach it from anywhere** | Works behind a Cloudflare Tunnel or any reverse proxy, with secure cookies and the visitor's real address used for rate limits. |
| 📱 **Phone-friendly** | Works in any phone browser, with a bottom menu; "Add to Home screen" gives you an app icon. |
| 🎨 **Looks the part** | Navy frosted glass with a violet or coral accent, tooltips on everything, and a `/` shortcut to search. |

## Screenshots 📸

| | |
| --- | --- |
| <img src="docs/screenshots/title-downloading.png" alt="A film's page with the journey tracker showing live download progress"> | <img src="docs/screenshots/requests.png" alt="The requests list with a journey tracker on every request"> |
| **Live progress** on a film's page | **Every request**, and where it's up to |
| <img src="docs/screenshots/people.png" alt="The People page with accounts and roles"> | <img src="docs/screenshots/account-created.png" alt="A new account's details ready to copy and send"> |
| **People:** accounts and what each person can do | **New account:** details ready to send to a friend |
| <img src="docs/screenshots/setup-wizard.png" alt="The setup wizard having found Radarr automatically"> | <img src="docs/screenshots/health.png" alt="The health page with plain-English checks"> |
| **Setup wizard:** finds Radarr and Sonarr for you | **Health:** what's working, and how to fix what isn't |

<p align="center"><img src="docs/screenshots/phone-home.png" width="260" alt="Encorr's home page on a phone"> &nbsp; <img src="docs/screenshots/phone-requests.png" width="260" alt="The requests list on a phone"></p>

## Get started (Windows) 🚀

1. **[Download Encorr-Windows.zip](https://github.com/FiTH-g1gGLe/Encorr/releases/latest/download/Encorr-Windows.zip)** and unzip it. The link always gives you the newest version.
2. **Double-click `encorr.exe`.** Your browser opens at <http://localhost:5150>. If Windows asks about the firewall, allow private networks.
3. **Follow the setup wizard:** sign in with Plex, pick your server and libraries, and press Test on Radarr and Sonarr (usually already filled in).
4. **Run it in the background:** go to Settings → General → *Run in the background* → **Turn on**, and press Yes when Windows asks. Encorr now starts with the computer and restarts itself if it ever stops.
5. **Invite people:** go to People → *Add a person*, then copy the message and send it.

**Updating:** download the new version and double-click it. It updates the background copy for you.

Encorr keeps its data in `C:\ProgramData\Encorr` (Linux: `~/.config/encorr`). Passwords are stored as slow salted hashes, and API keys and tokens are encrypted.

### Letting friends in from outside your home 🌍

Out of the box Encorr only works **inside your home** (`http://localhost:5150` on the server). To let friends request from their own homes or phones, Encorr needs a web address on the internet, such as `https://encorr.yourdomain.com`, that leads back to your server.

The easy, safe way is a free **[Cloudflare Tunnel](https://developers.cloudflare.com/tunnel/)**. A small Cloudflare program on your server connects **out** to Cloudflare, and visitors come back in through that connection:

```
Friend's phone ──► https://encorr.yourdomain.com ──► Cloudflare ══ tunnel ══► your server ──► Encorr
```

- 🔒 **No router changes**: no ports opened, and your home IP stays hidden.
- 🔐 **Free HTTPS padlock**, so passwords are encrypted on the way.
- 💷 **Free**: you only need a domain name (about £8–10 a year).
- 🔁 **Survives restarts**: it runs as a Windows service, like Encorr's background mode.

**The short version** (about 20–30 minutes):

1. Turn on **Settings → General → Run in the background**, so Encorr is always up.
2. Add your domain to a free [Cloudflare](https://dash.cloudflare.com) account and switch its nameservers to Cloudflare's.
3. In Cloudflare go to **Networking → Tunnels → Create Tunnel**, pick Windows, and run the install command it gives you on the server. ⚠️ Keep that command's long code private: it's the key to your tunnel.
4. Add a **published application route**: subdomain `encorr`, your domain, type **HTTP**, URL **`localhost:5150`**.
5. In Encorr set **Settings → General → Public address** to `https://encorr.yourdomain.com`, so invite messages carry the right link.
6. Turn Wi-Fi off on your phone and open the address to check it works.

Only Encorr goes on the internet: Radarr, Sonarr, Prowlarr and SABnzbd stay private. Encorr protects the sign-in page with hashed passwords, a lock-out after 10 wrong guesses, and secure cookies.

📖 **[Full step-by-step guide, with safety tips, troubleshooting and alternatives →](docs/REMOTE-ACCESS.md)**

### Options

| Option | What it does |
| --- | --- |
| `-port 5150` | Port for the web page |
| `-config <folder>` | Where Encorr keeps its data |
| `-no-browser` | Don't open the browser on start |
| `encorr install` / `encorr uninstall` | Turn background mode on or off from the command line |
| `-version` | Print the version |

## What's been built so far ✅

- [x] Setup wizard with Plex sign-in, auto-detected Radarr, Sonarr and SABnzbd, and a built-in TMDB key
- [x] Discover, search, title pages, trailers, seasons and "more like this"
- [x] Requests with approval, quotas, notes, retry and cancel
- [x] Journey tracker, live SABnzbd progress and a Completed tab
- [x] Instant Plex scan and fast "ready" detection (works with old and new Plex agents)
- [x] Encorr accounts (username + password) alongside Plex sign-in
- [x] Discord notifications and Plex watchlist requests
- [x] Health page and background jobs
- [x] Windows background service with one-click on/off and double-click updates
- [x] Friendly redesign: labelled menu, home page guide, tooltips and emojis
- [x] Tested end to end against simulated Plex, TMDB, Radarr, Sonarr, SABnzbd and Discord, plus a real Windows 11 server

## Coming next 🛣️

- [ ] Android app (request, approve and get notified on your phone)
- [ ] Discord bot (request from Discord)
- [ ] Invite links and "report a problem" with a title
- [ ] Tray icon and code-signed builds
- [ ] Mac support

## Building from source 🛠️

Needs Go 1.24 or newer.

```sh
go build -o encorr ./cmd/encorr                                    # this machine
GOOS=windows GOARCH=amd64 go build -o encorr.exe ./cmd/encorr      # Windows
go test ./...                                                       # tests
```

Every push to `main` is tested and published as a release, with the TMDB key added from a repository secret (it's never in the code).

### Trying it without a real server

`encorr-fake` pretends to be Plex, TMDB, Radarr, Sonarr, SABnzbd and Discord:

```sh
go run ./cmd/encorr-fake -port 5199 &
ENCORR_DEV_FAKE=http://localhost:5199 go run ./cmd/encorr -config /tmp/encorr-dev
```

Use TMDB key `good-key`. For Radarr use `http://localhost:5199/radarr` and key `radarr-key` (Sonarr: `/sonarr` and `sonarr-key`; SABnzbd: `/sabnzbd` and `sab-key`).

## Project layout 🗂️

| Folder | What's in it |
| --- | --- |
| `cmd/encorr` | The program: web server, background jobs and the Windows service |
| `internal/app` | Request engine, jobs, accounts, health checks and the `/api/v1` API |
| `internal/arr` | Radarr and Sonarr client, including auto-detection |
| `internal/sab` | SABnzbd client for live download progress |
| `internal/plex` | plex.tv sign-in, servers, friends, watchlists, library scans |
| `internal/tmdb` | Film and TV information from TMDB |
| `internal/notify` | Discord notifications |
| `internal/store` | The data file, with encrypted secrets and backups |
| `internal/web` | The web interface (built into the program) |
| `internal/fake` | Pretend services for tests and previews |

## Credits 🙏

Encorr's request flow follows ideas proven in [Seerr](https://github.com/seerr-team/seerr) (MIT), with ideas from Ombi, Watchlistarr, Requestrr, Doplarr, Reiverr and MediaManager. No code from GPL or AGPL projects is included. Quality advice comes from the [TRaSH Guides](https://trash-guides.info). Film and TV data comes from [TMDB](https://www.themoviedb.org); Encorr uses the TMDB API but is not endorsed or certified by TMDB.

## Licence

MIT © 2026 g1gGLe. See [LICENSE](LICENSE).
