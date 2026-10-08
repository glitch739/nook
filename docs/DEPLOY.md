# Running Nook, and reaching it from anywhere

Nook runs as a Docker container built from this repository. Source
code and the running instance are kept apart:

```
~/src/nook             source code: all development happens here
~/applications/nook    the running instance: nothing is edited here by hand
  ├── docker-compose.yml   copied from deploy/ in the repo
  ├── .env                 this installation's settings (private, chmod 600)
  ├── backup.sh            nightly backup, run by cron
  ├── backup.log
  └── data/                database, uploads, backups (owned by you)
      ├── nook.db
      ├── uploads/
      └── backups/
```

The internet setup decided in [ADR 0008](adr/0008-remote-access.md)
adds Cloudflare in front, through an outbound tunnel. No port is opened
on your router and your home IP address is never published:

```
phone / laptop, anywhere
   │  HTTPS
   ▼
Cloudflare edge ── TLS, DDoS absorption
   │
Cloudflare Access ── email one-time code; only listed addresses get through
   │
cloudflared container (outbound connection only)
   │  http://nook:8080, inside the compose network
   ▼
nook container (read-only filesystem, no capabilities, data in ./data)
```

Two gates protect your notes: Cloudflare Access decides who may even
see the login page, and Nook's own login decides who you are.

Commands are run on the server unless marked **(browser)**.

---

## Part A — Running on the home network (done 2026-10-02)

This is the current state. Nook answers on `http://<server-ip>:8080`
from the home network only, from the `nook:latest` image.

Everyday commands, run in `~/applications/nook`:

```sh
docker compose ps                 # running? "healthy"?
docker compose logs -f nook       # live log (Ctrl-C to stop watching)
docker compose restart nook
docker compose down               # stop
docker compose up -d              # start
```

The container restarts by itself after a crash or a server reboot.

### Changing Nook (development flow)

1. Edit and test in `~/src/nook`. Testing never touches the real data:
   use a throwaway instance with its own database.
2. Commit, then build a new image:
   ```sh
   cd ~/src/nook && ./scripts/build-image.sh
   ```
   It is tagged `nook:<date>-<commit>` and `nook:latest`. A `-dirty`
   suffix means it was built with uncommitted changes.
3. Back up, then switch the running instance to the new image:
   ```sh
   cd ~/applications/nook
   ./backup.sh
   docker compose up -d           # recreates the container from nook:latest
   docker compose ps
   ```
4. If the new version misbehaves, go back to the previous image:
   ```sh
   docker images nook             # find the previous tag
   docker tag nook:<previous-tag> nook:latest
   docker compose up -d
   ```
   Database migrations only go forward. If the bad version already
   changed the database, also restore the backup from step 3.

If `deploy/docker-compose.yml` or `deploy/.env.example` changed in the
repo, copy the compose file over again. Merge new `.env` lines by hand,
because `.env` holds your own settings.

### Backups

`backup.sh` runs every night at 03:30 from your user's crontab
(`crontab -l` shows it). It writes
`data/backups/nook-backup-<date>-<time>.tar.gz`, named in Budapest time, with a consistent snapshot
of the database and all uploads, keeps the newest 14, then removes uploaded
images and files nothing references any more (`nook sweep-uploads`: unused
for 7+ days, so undo still works), and logs to
`backup.log`. You can run it any time with `./backup.sh`.

**These copies are on the same disk.** They protect against mistakes
and corruption, not against disk failure, theft, or ransomware that
encrypts the whole machine. At least once a week, copy the newest file
somewhere the server can't write to afterwards:

- a USB drive you plug in, copy to, and unplug, or
- from your Mac:
  `scp you@<server-ip>:applications/nook/data/backups/<file> ~/NookBackups/`

**Restore**. Practice this once now, so you know it works:

```sh
cd ~/applications/nook
docker compose down
mv data data-before-restore
mkdir data && chmod 700 data
tar -xzf data-before-restore/backups/nook-backup-<time>.tar.gz -C data
mkdir -p data/backups && cp data-before-restore/backups/* data/backups/
docker compose up -d
```

When you are happy with the result, delete `data-before-restore`.

`migration/` holds the one-time snapshot taken when the data moved over
from the old development server on 2026-10-02. Keep it until the first
weeks of Docker use went fine.

---

## Part B — Reaching it from anywhere

### 1. Things you need first

1. **A domain.** Done: `nook.example.com`, registered at your registrar on
   2026-10-02 (`nook.hu` was taken). Nook lives on the domain itself,
   `https://nook.example.com`, with no subdomain.
2. **A free Cloudflare account** at cloudflare.com.

### 2. Put the domain on Cloudflare (browser)

1. In Cloudflare: *Add a domain*, enter it, pick the **Free** plan.
2. Cloudflare shows two nameservers. At your registrar, in the domain's
   settings, replace its nameservers with those two. This takes minutes to a day.
   Cloudflare emails you when the domain is active.
3. Once active, under *SSL/TLS* → *Edge Certificates*:
   - **Always Use HTTPS**: on.
   - **Minimum TLS Version**: 1.2.
4. Under *Network*: **WebSockets** on. It is on by default, and live
   collaborative editing needs it.

### 3. Update the server

The machine has pending updates and a pending reboot:

```sh
sudo apt update && sudo apt full-upgrade -y
sudo reboot
```

Nook comes back by itself after the reboot. Check with
`docker compose ps` in `~/applications/nook`.

### 4. Create the tunnel (browser, then server)

1. **(browser)** Cloudflare dashboard → *Zero Trust* (the first visit
   asks for a team name and the free plan) → *Networks* → *Tunnels* →
   **Create a tunnel** → type *Cloudflared* → name it `nook`.
2. **(browser)** On the install screen pick **Docker**. It shows a
   `docker run ... --token <LONG-TOKEN>` command. **Don't run it.**
   Copy only the token. It's a secret: don't paste it anywhere except
   the `.env` file below.
3. **(browser)** Continue to *Public Hostnames* and add one:
   - Subdomain: leave empty. Domain: `nook.example.com`
   - Service type **HTTP**, URL **`nook:8080`**
   - Leave *HTTP Host Header* empty.

   Don't open the address yet. Set up Access first (step 5), so Nook
   is never reachable without it.

### 5. Put Cloudflare Access in front (browser)

Access can only be attached to an **active** Cloudflare domain: while
the domain still says "pending nameserver update", `nook.example.com` is
missing from the Domain list. Until Access is in place, keep the
tunnel container stopped (`docker compose --profile tunnel stop
cloudflared`), so the domain can't go live unprotected the moment the
nameservers switch.

*Zero Trust* → *Access* → *Applications* → **Add an application** →
*Self-hosted*:

- Application domain: `nook.example.com` (subdomain empty)
- Session duration: **1 month**, which is how often you re-enter an
  email code
- Identity provider: **One-time PIN**. Cloudflare emails you a code,
  and there is nothing else to set up. Check that it really is in the
  list under *Integrations* → *Identity providers*: a new account may
  only have **Cloudflare** there, which makes everyone log in with a
  Cloudflare account. Add *One-time PIN* if it is missing.
- *Login methods* tab of the application: turn **off** "Accept all
  available identity providers", select only **One-time PIN**, and turn
  **on** "Apply instant authentication". The login page then shows the
  email field straight away.
- Add a policy: *Action* **Allow**, *Include* → **Emails** → your
  address, plus the address of everyone else who should use Nook.

Then a second, small application so phones can fetch the home-screen
icon. iOS fetches it without the Access cookie, so behind Access it
gets the login page instead and shows no icon. These files hold only
the icon and the app name. *Add an application* → *Self-hosted* →
*Public DNS*:

- Five public hostnames, domain `nook.example.com`, subdomain empty, paths
  `apple-touch-icon.png`, `icon-192.png`, `icon-512.png`,
  `manifest.webmanifest` and `favicon.svg`
- A policy named e.g. `public icons`: *Action* **Bypass**, *Include* →
  **Everyone**

Access applies the most specific path, so only these five files skip
the login. Check from a machine with no Access cookie: those five
return `200`, and `/` and `/api/me` still return `302` to Access.

### 6. Switch Nook to internet mode (server)

Edit `~/applications/nook/.env` (`nano .env`). Delete these three lines:

```
NOOK_PUBLIC_URL=http://<server-ip>:8080
NOOK_COOKIE_SECURE=false
NOOK_DEV_MAIL=log
```

and add:

```
NOOK_PUBLIC_URL=https://nook.example.com
NOOK_PUBLISH=127.0.0.1:8080
CLOUDFLARE_TUNNEL_TOKEN=<the token from step 4>
```

What this changes:
- An https public URL turns on HSTS and secure-only cookies. New
  sign-ups need an admin's approval (already set in `.env` since
  2026-10-02 as `NOOK_REGISTRATION=approval`); admins see a "waiting"
  badge next to their name.
- `NOOK_PUBLISH=127.0.0.1:8080` stops the home network from reaching
  Nook directly. Docker's published ports go around the ufw firewall,
  so this is the setting that actually closes the direct way in. From
  now on, use the https address at home too.
- Password-reset emails need SMTP settings (see `.env`). Without them,
  an admin creates reset links in Settings → Users.

Then start Nook together with the tunnel:

```sh
cd ~/applications/nook
docker compose --profile tunnel up -d
docker compose --profile tunnel ps          # both containers "Up"
docker compose logs cloudflared | tail      # "Registered tunnel connection"
```

The tunnel should now show **Healthy** in the dashboard. From now on,
start and stop with `--profile tunnel`, otherwise only Nook starts.

### 7. Final checks

- [ ] On a phone on mobile data (not your Wi-Fi), the address shows
      the Access email-code page, then Nook. Live editing works: the
      "Saved" indicator, other people's cursors.
- [ ] In a private window, an email that is *not* on the policy gets
      no code.
- [ ] `curl -sI https://nook.example.com` from anywhere answers
      with a redirect to Cloudflare Access, not with Nook.
- [ ] `http://<server-ip>:8080` no longer loads from another device
      at home.
- [ ] Your router has **no** port forwarding to this machine.
- [ ] `docker compose logs nook | grep login_succeeded` shows your own
      login with your real public IP, not a 172.31.80.x address. This
      proves the trusted-proxy setting works.
- [ ] In Nook's Settings → Users, every account is one you expect.
- [ ] You did one test restore (Part A, Backups) and copied one backup
      off the machine.

---

## If something looks wrong

What counts as wrong:
- logins you didn't make (`docker compose logs nook | grep login_`),
- unknown users or sessions in Settings,
- unknown emails in the Access logs (*Zero Trust* → *Logs* → *Access*).

What to do:

1. Cut internet access at once: `docker compose stop cloudflared`.
   Nook keeps running, and nothing is lost.
2. Back up the current state for investigation: `./backup.sh`.
3. In Nook's Settings → *Sessions*, sign out other devices for every
   account. Change passwords.
4. Look at what happened: `docker compose logs nook --since 72h` shows
   logins with IPs and lockouts. Also check the activity panel on
   pages and the Access logs.
5. Rotate the tunnel token: *Tunnels* → the tunnel → refresh the
   token. Put the new one in `.env`, then run
   `docker compose --profile tunnel up -d`.
6. If data was changed or deleted, restore the last good backup from
   before the incident.
