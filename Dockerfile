FROM golang:1.27.0 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY templates/ ./templates/
COPY migrations/ ./migrations/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/screamtober .
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/screamtober /screamtober
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV ADDR=:8080
ENV DATABASE_PATH=/data/screamtober.db
EXPOSE 8080
ENTRYPOINT ["/screamtober"]
