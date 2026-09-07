# Screamtober

Screamtober is a web application for an annual October movie challenge: build a
31-movie watchlist, track viewing progress, and rate movies, with challenge history
organized by year.

Built with Go and server-rendered HTML. The project currently serves a placeholder
page; accounts, movie tracking, and database storage are still to come.

## Run locally

Install Go 1.27.0 (see `go.mod`), then run from the repository root:

```sh
go run .
```

Open [localhost:8080](http://127.0.0.1:8080). Restart the app after changing code or
templates. To use a different address:

```sh
ADDR=127.0.0.1:3000 go run .
```

## Run with Docker

Build and start the container from the repository root:

```sh
docker build -t screamtober .
docker run --rm --name screamtober -p 127.0.0.1:8080:8080 screamtober
```

Open [localhost:8080](http://127.0.0.1:8080). Rebuild the image after making changes.
To stop the container, run this in another terminal:

```sh
docker stop screamtober
```

## Development checks

```sh
go test ./...
go vet ./...
```
