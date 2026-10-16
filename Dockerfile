# Build stages run on the build host's platform; only the final stage is per target platform.
# Build the production frontend; vite writes to /src/backend/internal/platform/webui/dist.
FROM --platform=$BUILDPLATFORM node:24.21.0-bookworm-slim AS web
WORKDIR /src/frontend
COPY api/generated/ /src/api/generated/
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
RUN npm run build && test -f /src/backend/internal/platform/webui/dist/index.html

# Build using the project-pinned Go toolchain; cross-compile a static binary for the target platform.
FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/backend/internal/platform/webui/dist/ ./internal/platform/webui/dist/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/tendo ./cmd/tendo

FROM gcr.io/distroless/base-debian13:nonroot@sha256:a0d70d6a97cd697d9362bc2aae4a6560dd65817e365d0043b07325a97975dc91
COPY --from=build /out/tendo /tendo
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/zoneinfo.zip
ENV ZONEINFO=/usr/local/go/lib/time/zoneinfo.zip
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tendo"]
