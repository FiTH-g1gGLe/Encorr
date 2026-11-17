<p align="center"><img src="encorr-logo.svg" width="300" alt="Encorr"></p>

# Letting friends in from outside your home 🌍

Out of the box, Encorr only answers on your home network: `http://localhost:5150` on the server, or `http://<server-name>:5150` from other devices in the house. That's on purpose, because nothing is exposed until you choose to expose it.

To let friends request from their own homes or phones, Encorr needs a **web address on the internet** that leads back to your server. This guide shows the recommended way, a free **Cloudflare Tunnel**, step by step, then covers the alternatives.

- [How a Cloudflare Tunnel works](#how-a-cloudflare-tunnel-works)
- [What you need](#what-you-need)
- [Step-by-step setup](#step-by-step-setup) (about 20–30 minutes)
- [Checking it works](#checking-it-works)
- [Keeping it safe](#keeping-it-safe)
- [Troubleshooting](#troubleshooting)
- [Other ways to do it](#other-ways-to-do-it)

---

## How a Cloudflare Tunnel works

```
 Friend's phone ──► https://encorr.yourdomain.com ──► Cloudflare ══ tunnel ══► your server ──► Encorr (localhost:5150)
```

A small Cloudflare program (`cloudflared`) runs on your server and opens a connection **out** to Cloudflare. When a friend visits your address, Cloudflare sends the visit back down that connection to Encorr.

Why this is the recommended way:

| | |
| --- | --- |
| 🔒 **No router changes** | You don't open or forward any ports. From the outside, your home network looks exactly as it did before. |
| 🕵️ **Your home IP stays hidden** | Visitors only ever see Cloudflare's address, never yours. |
| 🔐 **Free HTTPS** | Cloudflare gives your address the padlock automatically, so passwords are encrypted on the way. |
| 🛡️ **Protection built in** | Cloudflare filters out a lot of junk traffic and attacks before it reaches you. |
| 💷 **Free** | The tunnel costs nothing. You only need a domain name (about £8–10 a year). |
| 🔁 **Survives restarts** | `cloudflared` installs as a Windows service, so it comes back by itself after a reboot. |

---

## What you need

- **Encorr running in the background.** Settings → General → *Run in the background* → **Turn on**. Otherwise friends get an error whenever the Encorr window is closed.
- **A domain name**, for example `yourname.co.uk`. You can buy one from Cloudflare itself, or use one you already own from any company.
- **A free Cloudflare account** at [dash.cloudflare.com](https://dash.cloudflare.com).
- About **20–30 minutes**, most of it waiting for the domain to switch over.

> 💡 You'll use a **subdomain** such as `encorr.yourname.co.uk`, so the domain can still be used for other things.

---

## Step-by-step setup

### Part 1: Put your domain on Cloudflare

Skip this part if your domain already shows as **Active** in Cloudflare.

1. Sign in at **[dash.cloudflare.com](https://dash.cloudflare.com)** and click **Add a domain** (sometimes "Onboard a domain").
2. Type your domain, choose the **Free** plan and continue.
3. Cloudflare scans for existing DNS records. If it finds none, that's fine: press **Continue** and confirm. You don't need any records yet, because the tunnel creates the one it needs.
4. Cloudflare shows you **two nameservers**, like `ada.ns.cloudflare.com` and `bob.ns.cloudflare.com`. Keep this page open.
5. In another tab, sign in to the company you bought the domain from (Namecheap, GoDaddy, 123-reg, Porkbun and so on). Find **Nameservers** for your domain, choose **Custom**, replace what's there with Cloudflare's two, and save.
   - While you're there, check the domain hasn't **expired**.
6. Back in Cloudflare, click **Check nameservers now**. It usually turns **Active** within 10–30 minutes (occasionally up to a day), and Cloudflare emails you when it does.

### Part 2: Create the tunnel

7. In Cloudflare, open **Networking → Tunnels** from the left menu and click **Create Tunnel**.
8. Name it `encorr` and click **Create Tunnel**.
9. Choose **Windows** (64-bit). Cloudflare shows:
   - a link to **download `cloudflared`** for Windows, and
   - a command like `cloudflared.exe service install eyJhIjoi…`

   > ⚠️ That long code is the **key to your tunnel**. Anyone who has it could run your tunnel and serve their own pages on your address. Don't paste it into chats, screenshots or forums. If it ever leaks, refresh the token in the tunnel's settings and reinstall with the new one.

10. On the server, download and install **cloudflared** from that link.
11. On the server, press **Start**, type `cmd`, right-click **Command Prompt** and choose **Run as administrator**. Copy the whole command straight from the Cloudflare page, paste it in and press Enter. You should see `Agent service for cloudflared installed successfully`.
12. Back on the Cloudflare page, within a few seconds the tunnel shows as **Healthy** (or Connected).

### Part 3: Point an address at Encorr

13. Open the tunnel and go to **Routes → Add route → Published application**. Older layouts call this **Public Hostname**.
14. Fill it in:

    | Field | What to enter |
    | --- | --- |
    | **Subdomain** | `encorr` (or anything you like: `requests`, `films`…) |
    | **Domain** | your domain, e.g. `yourname.co.uk` |
    | **Path** | leave empty |
    | **Service type** | `HTTP` |
    | **URL** | `localhost:5150` |

15. Save. Cloudflare creates the DNS record for you.

> ℹ️ Use `HTTP` here, not `HTTPS`. The connection from Cloudflare to your server travels inside the encrypted tunnel; Cloudflare adds HTTPS for your visitors.

### Part 4: Tell Encorr its new address

16. In Encorr, go to **Settings → General → Public address** and enter `https://encorr.yourname.co.uk`, then save.

    This address is used in the account details Encorr gives you to send to friends, in Discord messages, and in links back to Encorr.

17. **Invite people:** go to **People → Add a person**, press **Copy message** and send it. It now contains your public address.

---

## Checking it works

1. On your phone, **turn Wi-Fi off** (so you're really coming in from outside) and visit `https://encorr.yourname.co.uk`.
2. You should see the Encorr sign-in page with the 🔒 padlock.
3. Sign in and request something small to confirm the full round trip.
4. Optional: restart the server and check the address works again once it's back. `cloudflared` and Encorr's background mode both start by themselves.

---

## Keeping it safe

Once Encorr has a public address, anyone on the internet can **reach** the sign-in page, though only people with an account can get **in**. Encorr is built for this:

- 🔐 **Passwords** are stored as slow, salted hashes, never as text.
- 🚦 **Wrong guesses** are limited: 10 wrong passwords from the same address locks it out for 15 minutes, and a wrong username takes as long to check as a wrong password, so nobody can fish for account names.
- 🍪 **Sessions** use secure, HTTP-only cookies.
- 🧱 **Settings** pages and setup are owner-only, and actions are protected against cross-site tricks.
- 🌐 Encorr reads the visitor's **real address** from Cloudflare, so these limits apply to the actual person and not to the tunnel.

Good habits on your side:

- Give friends the **Friend** role (their requests wait for your approval) unless you trust them fully.
- Use a strong password for your own owner account, and don't reuse it elsewhere.
- Keep the **tunnel key** private (see the warning in Part 2).
- **Don't expose Radarr, Sonarr, Prowlarr or SABnzbd** through the tunnel. Only Encorr needs to be public; it talks to the others privately on the server.

**Extra lock (optional):** Cloudflare's free **Zero Trust → Access** can put an extra login (such as an emailed one-time code) in front of the whole address, so strangers can't even see the sign-in page. It's handy if you only ever have a few known friends. Note that it adds a step for them every so often.

---

## Troubleshooting

| What you see | What it usually means | What to do |
| --- | --- | --- |
| **Error 1033** (Cloudflare page) | The tunnel isn't connected | Check the **Cloudflared** service is running in Windows *Services*, and the tunnel shows **Healthy** in Cloudflare. |
| **Error 502 / Bad gateway** | The tunnel works, but Encorr isn't answering | Is Encorr running? Open `http://localhost:5150` on the server. Turn on background mode so it's always running. |
| **The domain doesn't load at all** | Nameservers haven't switched yet, or the route is missing | Wait for Cloudflare to say **Active**; check the route in Part 3. |
| **Invite messages show `localhost`** | The public address isn't set | Settings → General → **Public address** (Part 4). |
| **Works at home, not on mobile data** | You tested on Wi-Fi, or the phone has cached an old DNS answer | Turn Wi-Fi off, and wait a few minutes after creating the route. |
| **Plex sign-in pop-up doesn't come back** | The browser blocked the pop-up | Allow pop-ups for your Encorr address, or use the username and password sign-in. |

---

## Other ways to do it

| Option | Good for | Trade-offs |
| --- | --- | --- |
| **Cloudflare Tunnel** (this guide) | Most people | Needs a domain (about £8–10 a year). Traffic passes through Cloudflare. |
| **Tailscale** | Just you and a few tech-confident friends | Very private, with no public address at all. But every friend must install the Tailscale app and join your network. |
| **Reverse proxy** (Caddy, Nginx Proxy Manager, Traefik) with port 443 forwarded | People already running one | You open a port on your router, and your home IP is visible. You manage certificates (Caddy does this automatically). |
| **Forwarding port 5150 on your router** | ❌ Not recommended | No encryption (passwords travel in plain text), and your server is directly exposed to the internet. |

Whichever you choose, set **Settings → General → Public address** to the address friends use.
