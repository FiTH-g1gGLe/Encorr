# Testing Encorr on your server

This takes about 20 minutes. Encorr runs **alongside** Ombi (Ombi uses port 5000, Encorr uses 5150), so nothing you have now is touched.

## Before you start

- No TMDB key needed: Encorr has one built in.
- **Optional, a Discord webhook**: in your Discord server, open a channel's settings → Integrations → Webhooks → New Webhook → Copy Webhook URL.

## 1. Start it

1. Put `encorr.exe` in `C:\Encorr\` and double-click it.
2. If SmartScreen warns "Windows protected your PC", click **More info → Run anyway**. (The build isn't code-signed yet.)
3. If the firewall asks, tick **Private networks** and allow it.
4. A black window shows the log, and your browser opens at <http://localhost:5150>. Keep the black window open while testing.

## 2. Setup wizard

| Step | What to do | What should happen |
| --- | --- | --- |
| Welcome | Sign in with Plex | A Plex window opens; approve it and you're back in the wizard |
| Server | Press **Use this** on your server | "Connected to …" |
| Libraries | Leave them all on, press Continue | |
| Radarr | It should find Radarr by itself | Address and key filled in, "Connected to Radarr", your quality profiles and folders in the dropdowns |
| Sonarr | Same as Radarr | "Connected to Sonarr" |
| Discord | Paste the webhook, press **Send test message** | A test message appears in the channel |
| Done | Press Start using Encorr | The Discover page with posters |

If Radarr or Sonarr isn't found automatically, enter the address (e.g. `http://localhost:7878`) and the API key from Radarr → Settings → General → API Key, then press Test.

## 3. Things to try

Tick these off as you go:

- [ ] **Badges**: after a minute, films you already have show **In Plex** on their posters.
- [ ] **Request a film**: pick something small you don't have, press Request. It should appear in Radarr straight away, tagged `encorr-<your name>`, and start searching.
- [ ] **Live download progress**: Encorr finds SABnzbd by itself (check Settings → SABnzbd). While a film downloads, its page and the Requests list show the step (Queued, Downloading, Unpacking, Moving into your library, Waiting for Plex), plus percentage, speed and time left, refreshing every 5 seconds.
- [ ] **Completed**: within a minute or two of Radarr importing it, the request moves from **In progress** to **Completed** (“In Plex”).
- [ ] **It arrives**: within about 5 minutes of Plex adding it, the request turns **In Plex** and Discord says "Your encore is ready".
- [ ] **Request TV seasons**: pick a show, untick some seasons. In Sonarr only the chosen seasons should be monitored.
- [ ] **Already in Plex**: open a film you have. There's no Request button, just "In Plex".
- [ ] **A friend signs in**: ask someone you share Plex with to open `http://<your server's IP>:5150` on your Wi-Fi and sign in with Plex. Their request should say "Waiting for approval", and you should see it under Requests with a number on the icon.
- [ ] **Approve it**: press Approve. It goes to Radarr/Sonarr.
- [ ] **Roles**: in Users, make your friend **Trusted**. Their next request should go straight through.
- [ ] **Watchlist**: on your Profile, turn on "Request from my watchlist", add a film to your Plex watchlist, then in Settings → Jobs press **Run now** on "Plex watchlist sync".
- [ ] **Health**: Settings → Health should be all green. Try stopping Radarr and pressing Re-check: you should get a clear message about what's wrong.
- [ ] **Phone**: open Encorr in Chrome on your OnePlus → menu → **Add to Home screen**.
- [ ] **Colour**: Settings → General, try violet and coral, then save the one you like.

## 4. What to send back

- What worked, what didn't, and anything that felt confusing.
- Screenshots of anything that looks wrong.
- The log file: `C:\ProgramData\Encorr\logs\encorr.log`. It doesn't contain your API keys or tokens.

## Stopping and removing it

Close the black window to stop Encorr. To remove it completely, delete `encorr.exe` and the folder `C:\ProgramData\Encorr`. Anything it added to Radarr/Sonarr stays there, as normal.
