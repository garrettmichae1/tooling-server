# Official Go 1.27.0 image pinned by the registry manifest digest.
FROM --platform=$BUILDPLATFORM golang:1.27.0@sha256:4013ae0f9e7994f8535c58c811f8f863fbed38b72e0d51e6592156f758d66146 AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod ./
COPY cmd/studycontainer ./cmd/studycontainer
COPY internal/auth ./internal/auth
COPY internal/ratelimit ./internal/ratelimit
COPY internal/tools ./internal/tools
COPY internal/studyhost ./internal/studyhost
# There are no third-party Go dependencies or network steps in this build.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOTOOLCHAIN=local \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /out/studycontainer ./cmd/studycontainer

FROM scratch
COPY --from=build --chmod=0555 /out/studycontainer /studycontainer
USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/studycontainer"]
