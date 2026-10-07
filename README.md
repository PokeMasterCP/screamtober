# Screamtober

Screamtober is a self-hosted October movie challenge for 1–4 people. Pick up to
31 movies each year, track what your household has watched, and rate movies
from 1 to 10. Past years stay saved. Anyone can browse; invited members can rate.

## Self-host with Docker

You'll need Docker, persistent storage, a
[TMDB v3 API key](https://www.themoviedb.org/settings/api) (not the API Read Access
Token), and HTTPS through your own reverse proxy or Cloudflare Tunnel. SQLite is
included, so there's no separate database server to set up. Run one app instance.

### 1. Build and configure

```sh
git clone https://github.com/PokeMasterCP/screamtober.git
cd screamtober
docker build -t screamtober .

export ADMIN_TOKEN="$(openssl rand -hex 32)"
export TMDB_API_KEY='your-tmdb-v3-api-key'
```

Save the admin token in a password manager and reuse it when restarting or
upgrading. Supply secrets at runtime; keep them out of source control.

### 2. Choose a hosting option

**Your own HTTPS reverse proxy**

```sh
docker run -d --name screamtober --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -v screamtober-data:/data \
  -e ADMIN_TOKEN -e TMDB_API_KEY screamtober
```

Point your HTTPS reverse proxy to `http://127.0.0.1:8080`. If the proxy runs in a
container, use a shared Docker network and `http://screamtober:8080` instead of
publishing the app port. The app itself serves HTTP.

For local testing, add `-e AUTH_INSECURE_COOKIE=true` and open
[localhost:8080](http://127.0.0.1:8080). Use this setting only for local HTTP.

**Cloudflare Tunnel**

Create a remotely managed tunnel for your own domain, pointing to
`http://screamtober:8080`. Set `TUNNEL_TOKEN` to its connector token, then run:

```sh
docker network create screamtober-net
docker run -d --name screamtober --restart unless-stopped \
  --network screamtober-net -v screamtober-data:/data \
  -e ADMIN_TOKEN -e TMDB_API_KEY -e CLOUDFLARE_TUNNEL=true screamtober
docker run -d --name cloudflared --restart unless-stopped \
  --network screamtober-net -e TUNNEL_TOKEN \
  cloudflare/cloudflared:latest tunnel --no-autoupdate run
```

Keep the app private: no published app port or other public ingress, and only
trusted services on its network. The connector needs outbound access. Pin its
image version for repeatable deployments; configure edge protections separately.

### 3. Set up your household

Visit `/login` with your admin token. Add movies, arrange them on the October
calendar, and create one owner profile and up to three members. Copy each personal
login token when issued and share it privately; it is shown only once.

Sign out of admin, then sign in with your personal token to rate movies. Saving a
rating marks the movie watched for the household. You can change or remove your
own ratings; removing a movie's only rating marks it unwatched again. Admin access
manages the app; personal access is for ratings. There is no public registration.

## Settings

Pass settings with `-e NAME=value` when starting the container.

| Variable | Default / purpose |
| --- | --- |
| `ADMIN_TOKEN` | Required admin secret, at least 32 bytes. |
| `TMDB_API_KEY` | Required for movie search; saved challenges work without it. |
| `ADDR` | `:8080` in Docker. Adjust port and proxy settings if changed. |
| `DATABASE_DIR` | `/data` in Docker. Mount persistent storage here. |
| `CLOUDFLARE_TUNNEL` | `false`. Set `true` only for private tunnel ingress; trusts Cloudflare's client IP header but does not create a tunnel. Otherwise, forwarded IP headers are ignored. |
| `AUTH_INSECURE_COOKIE` | `false`. Set `true` only for local HTTP testing. |
| `LOG_LEVEL` | `info`; also accepts `debug`, `warn`, or `error`. |
| `TZ` | `UTC`. The household's IANA time zone, such as `America/New_York`; decides tonight's movie and the current challenge year. |

## Keep your data

- **Storage:** The examples save `screamtober.db` in the `screamtober-data` volume.
  Verify it is mounted at `/data` and keep it when replacing the container.
  Custom mounts must be writable by UID/GID `65532:65532`.
- **Backups:** Take regular SQLite-consistent backups using SQLite's `.backup`
  command, plus volume snapshots where available. Copying the live database file
  alone can miss writes in its WAL file. Test restoration before relying on backups.
- **Upgrades:** Back up, pull the latest source, rebuild, and recreate the container
  with the same settings and data volume. Database migrations run automatically.
- **Troubleshooting:** Check `docker logs screamtober`. Keep logs private; they can
  include movie search titles.

For developer tools and checks, see [DEVELOPMENT.md](DEVELOPMENT.md).
