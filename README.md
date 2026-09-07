# October Movie Challenge

A starting point for the annual movie challenge application, using Go's standard
library HTTP server and escaped HTML templates.

## Build and run with Docker

Build the image from the repository root:

```sh
docker build -t screamtober .
```

Run the container:

```sh
docker run --rm --name screamtober -p 127.0.0.1:8080:8080 screamtober
```

Open http://127.0.0.1:8080. Stop the container with `docker stop screamtober`
from another terminal. The image includes the HTML templates and runs as a
non-root user. Rebuild it after changing code or templates.

The container listens on `:8080`. Pass `-e LOG_LEVEL=debug` before the image name
to enable debug logging. Logs go to stdout and can be viewed with
`docker logs screamtober` while the container exists. Configure log retention
through your container runtime or hosting platform.

## Develop locally without Docker

Install Go 1.27.0 (see `go.mod`), then run:

```sh
go run .
```

Open http://127.0.0.1:8080. To change the listening address:

```sh
ADDR=127.0.0.1:3000 go run .
```

The home page is in `templates/home.html`. Templates are embedded in the binary,
so restart `go run .` after editing them. No external dependencies are required.

## Logging

The application uses Go's `log/slog` to emit single-line JSON to stdout, including
`time`, `level`, and `message` fields. Startup, template errors, and HTTP server
errors use this logger. Request access logging is not implemented yet.

`LOG_LEVEL` sets the minimum severity: `debug`, `info` (default), `warn`, or `error`.
An invalid value stops startup. For example:

```sh
LOG_LEVEL=debug go run .
```

No local log files or volume storage are needed. Keep credentials, session tokens,
request bodies, and other secrets out of log attributes.

## Checks

```sh
go test ./...
go vet ./...
```

This bootstrap serves a public placeholder page only. Authentication, movie data,
database storage, and hosted deployment are not implemented yet.
