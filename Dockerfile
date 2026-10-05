# Build using the project-pinned Go toolchain; keep generated binary independent of libc.
FROM golang:1.26.8-bookworm AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/tendo ./cmd/tendo

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/tendo /tendo
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/zoneinfo.zip
ENV ZONEINFO=/usr/local/go/lib/time/zoneinfo.zip
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tendo"]
