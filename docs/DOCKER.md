# Encorr on Linux: Docker, Portainer, or on its own

Encorr comes as a Docker image for normal PCs and servers (amd64) and ARM boards such as a Raspberry Pi 4 or 5 (arm64):

```
ghcr.io/fith-g1ggle/encorr:latest
```

Every release is also tagged with its version (for example `ghcr.io/fith-g1ggle/encorr:0.1.43`), if you'd rather stay on one.

## With Portainer

1. In Portainer, open **Stacks** and press **Add stack**.
2. Name it `encorr`, choose **Web editor**, and paste this:

   ```yaml
   services:
     encorr:
       image: ghcr.io/fith-g1ggle/encorr:latest
       container_name: encorr
       ports:
         - "5150:5150"
       environment:
         - TZ=Europe/London   # change to your time zone
       volumes:
         - encorr-data:/config
       restart: unless-stopped

   volumes:
     encorr-data:
   ```

3. Press **Deploy the stack**.
4. Open `http://YOUR-SERVER-IP:5150` in a browser (for example `http://192.168.1.50:5150`). The setup wizard starts: choose **Plex** or **Jellyfin** and follow it.

Everything Encorr knows (settings, people, requests) is kept in the `encorr-data` volume, so updates and restarts keep it.

### Updating

Portainer: open **Stacks**, then **encorr**, open the **Editor** tab and press **Update the stack** with **Re-pull image and redeploy** switched on. Your data stays.

Plain Docker: `docker compose pull && docker compose up -d`.

## Connecting to Jellyfin, Plex, Radarr, Sonarr and SABnzbd

Inside a container, `localhost` means the Encorr container itself, not your server. So when the wizard asks for an address:

- **Use your server's address on your network**, for example `http://192.168.1.50:8096` for Jellyfin, `http://192.168.1.50:7878` for Radarr, `http://192.168.1.50:8989` for Sonarr, `http://192.168.1.50:8080` for SABnzbd. This always works.
- **Or, if your apps are on a Docker network together**, put Encorr on the same network and use their container names, such as `http://jellyfin:8096` or `http://radarr:7878`. Add this to the stack (change `media` to your network's name):

  ```yaml
      networks:
        - media

  networks:
    media:
      external: true
  ```

Encorr's "find it for me" buttons look on the machine Encorr runs on, which in Docker is the container. They won't find your other apps, so type the addresses in.

**Instant scans after a download** work when Radarr/Sonarr and Jellyfin (or Plex) see your media at the same paths, which is the usual Docker setup (for example everything mounts your media at `/media` or `/data`). If the paths differ, nothing breaks: new files still appear when Jellyfin or Plex next notices them.

## Keeping data in a folder instead

Swap the volume for a folder on the server, such as `/opt/encorr`:

```yaml
    volumes:
      - /opt/encorr:/config
```

Encorr runs as user 1000 (the first user on most Linux systems), so that folder must belong to it: `sudo mkdir -p /opt/encorr && sudo chown 1000:1000 /opt/encorr`. To run as someone else, add `user: "1001:1001"` (their ids) under `encorr:` and give the folder to them.

## Reaching it from outside your home

Encorr works behind a reverse proxy (Nginx Proxy Manager, Traefik, Caddy) or a Cloudflare Tunnel. Point it at `http://encorr:5150` (same Docker network) or `http://YOUR-SERVER-IP:5150`. Then set **Settings → General → Public address** to it, so links in Discord messages work. More in [REMOTE-ACCESS.md](REMOTE-ACCESS.md).

## Linux without Docker

Download `Encorr-Linux-amd64.tar.gz` (or `-arm64` for ARM boards) from the [latest release](https://github.com/FiTH-g1gGLe/Encorr/releases/latest), then:

```bash
mkdir -p ~/encorr && tar -xzf Encorr-Linux-amd64.tar.gz -C ~/encorr
~/encorr/encorr -no-browser
```

Data goes to `~/.config/encorr` (or wherever `ENCORR_CONFIG` points). To start it with the computer, save this as `/etc/systemd/system/encorr.service`, changing `you` to your username:

```ini
[Unit]
Description=Encorr
After=network-online.target

[Service]
User=you
ExecStart=/home/you/encorr/encorr -no-browser
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Then `sudo systemctl enable --now encorr`.
