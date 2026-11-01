# Build stages run on the build host's platform; only the final stage is per target platform.
# Build the production frontend; vite writes to /src/backend/internal/platform/webui/dist.
FROM --platform=$BUILDPLATFORM node:26.13.0-bookworm-slim AS web
WORKDIR /src/frontend
COPY api/generated/ /src/api/generated/
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
RUN npm run build && test -f /src/backend/internal/platform/webui/dist/index.html

# Build using the project-pinned Go toolchain; cross-compile a static binary for the target platform.
FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/backend/internal/platform/webui/dist/ ./internal/platform/webui/dist/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/tendo ./cmd/tendo

FROM gcr.io/distroless/base-debian13:nonroot@sha256:9039a88709bcc2bacf5a2faac331d65dd6840f763f225ad9594cc0c67bcd0fba
COPY --from=build /out/tendo /tendo
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/zoneinfo.zip
ENV ZONEINFO=/usr/local/go/lib/time/zoneinfo.zip
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tendo"]
