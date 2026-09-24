# Screamtober

Screamtober is a self-hosted October movie challenge for a household of 1–4 people.
Build a watchlist of up to 31 movies, track shared viewing progress, and give each
movie a personal star rating. Challenges are saved by year so you can revisit
past Octobers. Anyone can browse; only invited household members can rate movies.

Anyone can run this open-source application for their own household. The
maintainer's Screamtober domain hosts their personal instance; self-hosters use
their own server and domain.

## Requirements

- A Docker-compatible host with persistent storage.
- A [TMDB v3 API key](https://www.themoviedb.org/settings/api) to search for and add
  movies. Use the API key, not the API Read Access Token.
- HTTPS for hosted use, provided by your reverse proxy or an optional Cloudflare
  Tunnel. Local testing can use HTTP.

SQLite is included; no separate database server is needed. Run one app instance.

## Run with Docker

Clone the repository and build the image:

```sh
git clone https://github.com/PokeMasterCP/screamtober.git
cd screamtober
docker build -t screamtober .
```

Generate an administrator token once and set your TMDB key:

```sh
export ADMIN_TOKEN="$(openssl rand -hex 32)"
export TMDB_API_KEY='your-tmdb-v3-api-key'
```

Save the admin token in a password manager and reuse it between runs. For hosted
use, supply both values through runtime secrets. Keep them out of source control,
and never share the admin token with household members.

### With your own HTTPS reverse proxy

```sh
docker run -d --name screamtober --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -v screamtober-data:/data \
  -e ADMIN_TOKEN -e TMDB_API_KEY screamtober
```

The app listens on all container interfaces on port 8080 (`ADDR=:8080`). The
`-p` option publishes that port on the host's loopback interface. For a proxy
running on the host, forward your own domain to `http://127.0.0.1:8080`.
For a proxy in another container, put both containers on the same user-defined
Docker network and forward to `http://screamtober:8080`; no published app port is
needed. The app itself serves HTTP.

For local testing, add `-e AUTH_INSECURE_COOKIE=true` to the command and open
[localhost:8080](http://127.0.0.1:8080). Leave this setting off for hosted HTTPS.

### With Cloudflare Tunnel

Create a remotely managed tunnel using a hostname on your own domain and point it
to `http://screamtober:8080`. Here, `screamtober` is the container name on the shared
Docker network. Set `TUNNEL_TOKEN` in your shell to the tunnel's connector token,
then run this **instead of** the reverse-proxy example:

```sh
docker network create screamtober-net
docker run -d --name screamtober --restart unless-stopped \
  --network screamtober-net -v screamtober-data:/data \
  -e ADMIN_TOKEN -e TMDB_API_KEY -e CLOUDFLARE_TUNNEL=true screamtober
docker run -d --name cloudflared --restart unless-stopped \
  --network screamtober-net -e TUNNEL_TOKEN \
  cloudflare/cloudflared:latest tunnel --no-autoupdate run
```

Keep the app private with no published port or other public ingress; only trusted
services should share its network. The connector needs outbound network access.
For repeatable deployments, pin a connector version or digest. Configure any
additional Cloudflare security protections separately.

## Configuration

Set these environment variables when starting the app; restart it after changes.

| Variable | Purpose / default |
| --- | --- |
| `ADMIN_TOKEN` | Required administrator secret, at least 32 bytes. |
| `TMDB_API_KEY` | TMDB v3 key for movie search. Existing challenges remain viewable without it. |
| `DATABASE_DIR` | Data directory: `/data` in Docker, `data` for local Go runs. Mount persistent storage here. |
| `ADDR` | Listen address: `:8080` in Docker, `127.0.0.1:8080` locally. Update proxy and port settings if changed. |
| `CLOUDFLARE_TUNNEL` | Default `false`. Set `true` only for private tunnel ingress to trust Cloudflare's client IP header; it does not create a tunnel. Otherwise, forwarded IP headers are ignored. |
| `AUTH_INSECURE_COOKIE` | Default `false`. Set `true` only for local HTTP testing. |

## Use Screamtober

1. **Set up your household.** Visit `/login` with your admin token. In household
   management, create one owner profile and up to three members. Copy each personal
   token when shown and share it privately; tokens cannot be retrieved later.
2. **Build a watchlist.** Choose **Search movies**, select a year, search for a
   title, and add your picks. Each year allows up to 31 entries, including repeats.
   Choose where you'll watch each pick, or leave it undecided. This records your
   choice; it does not check streaming availability.
3. **Arrange October.** Choose **Arrange movies**, drag posters onto calendar days,
   then choose **Save arrangement**. Drop onto an occupied day to swap movies, or
   back into the tray to unschedule one. Arrangement requires JavaScript and a
   pointer or touch device.
4. **Watch and rate.** Sign out of admin, then sign in with your personal token.
   Save a 1–5 star rating on a movie card to mark it watched for the household.
   You can edit your rating later. Everyone, including visitors, can see each
   person's scores, the household average, the top-rated pick, and how many
   movies each person has rated. Signed-in people also see watched movies they
   have not rated yet.
5. **Revisit past years.** Use the year links to browse previous challenges.
   Adding a new year's lineup preserves earlier years.

Use the admin panel to change viewing services, delete picks, or manage access.
Deleting a pick also deletes its ratings. **Replace login token** invalidates a
lost token; **Disable access** signs that person out while preserving their profile
and ratings. Disabled profiles still count toward the household limit.

Admin access manages the app; personal tokens—including the owner's—are for
ratings. Sign out before switching between them. There is no public registration.

Personal sign-ins stay active across restarts and upgrades. They end after 30 days
without use, or 90 days after signing in. Admin sessions last one hour, and
restarting the app signs admin out.

## Storage, backups, and upgrades

The Docker examples store `screamtober.db` in the named volume `screamtober-data`.
Verify that the volume is mounted at `/data` and retain it when replacing the
container. Custom mounts must be writable by UID/GID `65532:65532`; set
`DATABASE_DIR` if you use another mount location.

Take regular SQLite-consistent backups, for example with SQLite's `.backup`
command, and use volume snapshots where available. Do not simply copy the live
database file: recent writes may be in its WAL file. Test restoration to a separate
database before relying on a backup.

To upgrade, back up first, pull the latest source, rebuild the image, and recreate
the container with the same settings and data volume. Database updates apply
automatically at startup; the `database initialized` log entry lists any
`migrations_applied` and the resulting `schema_version`. Never delete or recreate
the database to upgrade.

If the app fails to start, check `docker logs screamtober` for configuration or
storage errors. Keep logs private; they can include movie search titles.

## Run without Docker

Install Go 1.27.0, clone the repository, and set the tokens above. From the
repository root, run:

```sh
AUTH_INSECURE_COOKIE=true go run .
```

Open [localhost:8080](http://127.0.0.1:8080). Data is saved in `data/screamtober.db`.
For development tools and checks, see [DEVELOPMENT.md](DEVELOPMENT.md).
