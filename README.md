<p align="center"><img src="docs/encorr-logo.svg" width="380" alt="Encorr"></p>

<p align="center"><b>Request the encore.</b><br>A friendly media request app for Plex. Friends ask for a film or show, and it turns up in Plex by itself.</p>

<p align="center">
  <a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/FiTH-g1gGLe/Encorr?label=release&color=9b7bff"></a>
  <a href="https://github.com/FiTH-g1gGLe/Encorr/actions/workflows/build.yml"><img alt="Build" src="https://img.shields.io/github/actions/workflow/status/FiTH-g1gGLe/Encorr/build.yml?branch=main&label=build"></a>
  <a href="https://github.com/FiTH-g1gGLe/Encorr/releases"><img alt="Downloads" src="https://img.shields.io/github/downloads/FiTH-g1gGLe/Encorr/total?color=34d399"></a>
  <a href="LICENSE"><img alt="Licence: MIT" src="https://img.shields.io/github/license/FiTH-g1gGLe/Encorr?color=60a5fa"></a>
  <img alt="Go" src="https://img.shields.io/github/go-mod/go-version/FiTH-g1gGLe/Encorr?logo=go&logoColor=white">
  <img alt="Windows and Linux" src="https://img.shields.io/badge/runs%20on-Windows%20%7C%20Linux-18181d">
  <img alt="No Docker needed" src="https://img.shields.io/badge/Docker-not%20needed-18181d">
  <img alt="Works with Plex, Radarr, Sonarr, SABnzbd" src="https://img.shields.io/badge/works%20with-Plex%20%C2%B7%20Radarr%20%C2%B7%20Sonarr%20%C2%B7%20SABnzbd-e5a00d">
</p>

<p align="center"><a href="https://github.com/FiTH-g1gGLe/Encorr/releases/latest/download/Encorr-Windows.zip"><b>⬇️ Download Encorr for Windows</b></a> &nbsp;·&nbsp; <a href="TESTING.md">🧪 Testing guide</a> &nbsp;·&nbsp; <a href="https://github.com/FiTH-g1gGLe/Encorr/releases">📝 What's new</a></p>

<p align="center"><img src="docs/screenshots/home.png" width="860" alt="Encorr home page: a greeting, a big search box, a rotating banner of trending films and rows of posters"></p>

## What is it? 🍿

Encorr is the front door to your Plex server. People sign in, find something to watch, and press **Request**. Encorr hands it to Radarr (films) or Sonarr (TV), SABnzbd downloads it, and Encorr tells them when it's ready to watch.

It does the same job as Overseerr, Jellyseerr and Ombi, but it's built to be:

- **🪶 Simple to run:** one program file. No Docker, no Node, no Java, no database server.
- **🧭 Simple to set up:** a wizard signs you in with Plex and finds Radarr, Sonarr and SABnzbd on the same machine by itself.
- **🙂 Simple to use:** plain English everywhere, big buttons, and a progress tracker anyone can read.
- **🌙 Always on:** one click runs it in the background as a Windows service that starts with the computer.

## Features ✨

### Finding something to watch

| | |
| --- | --- |
| 🎠 **Rotating banner** | The top 5 trending films and shows, with backdrop, rating and a one-tap Request. |
| 🧭 **Rows that change daily** | Trending, Just added to Plex, popular films and TV, coming soon, plus a fresh mix every day: studios (Marvel, Disney, Pixar, Ghibli…), Netflix, HBO and Apple TV+ originals, genres, decades, hidden gems, top rated, and an actor spotlight. |
| 🖼️ **See all** | Tap any row heading for a full poster page that keeps loading as you scroll. |
| 🔎 **Search on the home screen** | Results split into **Films** and **TV shows**. Posters show what's already in Plex or already requested. |
| ✨ **Poster peeks** | Hover a poster for its title, year, rating and a short description. |

### Film and show pages

| | |
| --- | --- |
| 🎬 **Rich details** | Crew (director, writers, creator, producers, music), cast with photos, tags, release dates (cinema, digital, disc), budget, revenue, language, country and studio. |
| ⭐ **Ratings** | TMDB for everything, plus IMDb and Rotten Tomatoes (critics and audience) from Plex for things in your library. |
| ▶️ **Play on Plex** | Opens it straight in Plex, and shows the quality you have (e.g. 4K HDR10, 5.1). |
| 📡 **Where it's streaming** | UK streaming services with their logos, plus links to TMDB, IMDb, Trakt, Letterboxd and TheTVDB. |
| 📚 **Collections** | "Part of the Harry Potter Collection": see the whole series and request every film you haven't got in one go. |
| 📺 **Season picker** | A table of seasons with switches, episode counts and what's already in Plex or on its way. |

### Requests and downloads

| | |
| --- | --- |
| 🗂️ **Four simple lists** | **Needs you** (approvals and problems), **Active**, **Coming soon** and **Ready**, shown as tiles with counts. |
| 🗺️ **Six-step tracker** | Requested → Approved → Finding → Downloading → Adding → Ready on every card, plus one line saying what's happening and what's next: when it'll search again, when it's out, or why it's stuck. |
| 📺 **Every episode** | A strip per season (in Plex, downloading, missing) and a full episode list from Sonarr with quality, air dates and a search button for a season or a single episode. |
| 🕑 **Timeline** | The whole story of a request: asked for, approved, copy found, failed, swapped, imported, upgraded. |
| 🎛️ **Download controls** | Pause, resume, move to front, try another copy, stop, import, search now, or **pick a copy yourself** from every release the indexers have. |
| 🩹 **Auto-fix** | Every 5 minutes Encorr imports downloads stuck before Plex, swaps failed or stalled ones for another copy and clears SABnzbd's leftovers. If nothing at all is moving it warns you instead of throwing good copies away. |
| ✅ **Ready** | When it arrived, a Play on Plex button, and a "new" badge for whoever asked. Tidies itself after 30 days. |
| 🔁 **Never forgotten** | Encorr asks Radarr/Sonarr to search again every few hours for anything with nothing downloading. |
| ⚡ **Instant "ready"** | When a download finishes, Encorr asks Plex to scan just that folder, so requests turn green within seconds. |
| 👀 **Plex watchlists** | Add something to your Plex watchlist and Encorr requests it. Remove the request and it stays removed. |

### Running it

| | |
| --- | --- |
| 👥 **Accounts for friends** | Username and password, or Plex sign-in. Owner, Trusted (no approval needed) and Friend (needs approval, with limits). |
| 📚 **Knows your library** | Reads your whole Plex library the moment it's connected, then keeps up every few minutes. |
| 💬 **Discord** | New requests, approvals, problems and "Your encore is ready", with @mentions. |
| ❤️ **Health page** | Plain-English checks for Plex, TMDB, Radarr, Sonarr, SABnzbd, indexers, Discord and disk space, with fix buttons (resume or restart SABnzbd, clear leftovers, search for everything missing) and a log of everything Encorr fixed. |
| 🌍 **Reach it from anywhere** | Works behind a Cloudflare Tunnel or reverse proxy. [Step-by-step guide](docs/REMOTE-ACCESS.md). |
| 📱 **Phone-friendly** | Works in any phone browser with a bottom menu; "Add to Home screen" gives you an app icon. |
| 🎨 **Your colours** | Midnight navy or graphite, with 8 accent colours: lavender, blue, teal, green, yellow, orange, red and pink. |

## Screenshots 📸

| | |
| --- | --- |
| <img src="docs/screenshots/title.png" alt="A film's page with crew, tags, ratings and a facts panel"> | <img src="docs/screenshots/requests.png" alt="Requests with filter tiles, a six-step tracker and season strips"> |
| **Film page:** crew, ratings, release dates, streaming and more | **Requests:** where everything is up to, and what happens next |
| <img src="docs/screenshots/episodes.png" alt="Every episode of a season with its status"> | <img src="docs/screenshots/manage.png" alt="Download controls: pause, move to front, try another copy, pick a copy"> |
| **Episodes:** what's in Plex, downloading and missing | **Download controls** for the owner |
| <img src="docs/screenshots/pick-copy.png" alt="Every release the indexers have, with quality, size and age"> | <img src="docs/screenshots/timeline.png" alt="A request's timeline"> |
| **Pick a copy** yourself | **Timeline:** the story of a request |
| <img src="docs/screenshots/posters.png" alt="A poster showing its title, rating and description on hover"> | <img src="docs/screenshots/collection.png" alt="A film collection with a button to request the missing films"> |
| **Poster peeks** and the "Just added to Plex" row | **Collections:** request the films you haven't got |
| <img src="docs/screenshots/search.png" alt="Search results split into films and TV shows"> | <img src="docs/screenshots/request-seasons.png" alt="The season picker with switches and status"> |
| **Search:** films and TV shows apart | **Season picker** |
| <img src="docs/screenshots/see-all.png" alt="A see-all page for one row"> | <img src="docs/screenshots/title-downloading.png" alt="A film downloading, with the journey tracker"> |
| **See all** for any row | **Live progress** on a film's page |
| <img src="docs/screenshots/people.png" alt="The People page with accounts and roles"> | <img src="docs/screenshots/settings-look.png" alt="Settings with 8 accent colours and two backgrounds"> |
| **People:** accounts and what each person can do | **Your colours:** 8 accents, black or navy |
| <img src="docs/screenshots/setup-wizard.png" alt="The setup wizard connected to Radarr"> | <img src="docs/screenshots/health.png" alt="The health page with plain-English checks"> |
| **Setup wizard** | **Health:** what's working, fix buttons, and what auto-fix did |

<p align="center"><img src="docs/screenshots/phone-home.png" width="240" alt="Encorr's home page on a phone"> &nbsp; <img src="docs/screenshots/phone-title.png" width="240" alt="A film's page on a phone"> &nbsp; <img src="docs/screenshots/phone-requests.png" width="240" alt="Requests on a phone"> &nbsp; <img src="docs/screenshots/phone-episodes.png" width="240" alt="Episodes on a phone"></p>

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
- [x] Home screen with a rotating banner, daily-changing rows, "see all" pages and split film/TV search
- [x] Rich film and show pages: crew, cast, tags, ratings, release dates, streaming services and "Play on Plex"
- [x] Collections, season picker, trailers and "more like this"
- [x] Requests with approval, limits, notes, retry and cancel
- [x] Requests in four lists (Needs you, Active, Coming soon, Ready) with a six-step tracker and live SABnzbd progress
- [x] Episode-by-episode view, season strips and per-request timelines
- [x] Download controls: pause, resume, move to front, try another copy, stop, pick a copy yourself, import
- [x] Auto-fix for stuck imports, failed and stalled downloads and SABnzbd leftovers, with an activity log
- [x] "Ready" badge for requesters, Play on Plex, and automatic re-searching for stalled requests
- [x] Instant Plex scan and fast "ready" detection (old and new Plex agents)
- [x] Encorr accounts (username + password) alongside Plex sign-in
- [x] Discord notifications and Plex watchlist requests
- [x] Health page with fix buttons, Radarr/Sonarr warnings, a stuck-download warning and background jobs
- [x] Windows background service with one-click on/off and double-click updates
- [x] Midnight or graphite themes with 8 accent colours
- [x] Tested end to end against simulated Plex, TMDB, Radarr, Sonarr, SABnzbd and Discord, plus a real Windows 11 server

## Coming next 🛣️

- [ ] "Report a problem" with a title, so friends can flag a bad file
- [ ] IMDb and Rotten Tomatoes ratings for everything (optional OMDb key)
- [ ] Android app (request, approve and get notified on your phone)
- [ ] Discord bot (request from Discord)
- [ ] Tray icon, code-signed builds and Mac support

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
