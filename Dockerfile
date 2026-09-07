FROM golang:1.27.0 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY templates/ ./templates/
RUN CGO_ENABLED=0 go build -trimpath -o /out/screamtober .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/screamtober /screamtober
USER 65532:65532
ENV ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/screamtober"]
