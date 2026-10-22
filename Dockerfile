# syntax=docker/dockerfile:1

# The metaX repository is a Provider workspace, not a standalone server. The
# builder applies its patches and overlays to the pinned MetaTube SDK checkout,
# runs the integration tests, and compiles the server binary.
ARG GO_VERSION=1.26.0
ARG SDK_REPOSITORY=https://github.com/metatube-community/metatube-sdk-go.git
ARG SDK_COMMIT=c62584675c593816cc07e7b5b977d14c7eb505a3

FROM golang:${GO_VERSION}-bookworm AS build

ARG SDK_REPOSITORY
ARG SDK_COMMIT

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/metax

# Keep the build context small and cache-friendly. The build script only reads
# these directories; docs and Git metadata are not used.
COPY .github ./.github
COPY provider ./provider
COPY routing ./routing
COPY patches ./patches
COPY third_party ./third_party

RUN git clone "$SDK_REPOSITORY" /src/metatube-sdk-go \
    && git -C /src/metatube-sdk-go checkout "$SDK_COMMIT" \
    && METAX_SKIP_UPSTREAM_TESTS=1 bash .github/scripts/build-metatube-server.sh \
        /src/metatube-sdk-go \
        /out/metatube-server

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 metax \
    && useradd --uid 10001 --gid metax --home-dir /data --shell /usr/sbin/nologin --no-create-home metax \
    && mkdir -p /data \
    && chown -R metax:metax /data

COPY --from=build /out/metatube-server /usr/local/bin/metatube-server

LABEL org.opencontainers.image.title="metaX backend" \
      org.opencontainers.image.description="MetaTube-compatible server with the metaX native providers" \
      org.opencontainers.image.source="https://github.com/strange-dragon/metaX"

ENV GIN_MODE=release

USER metax
WORKDIR /data
EXPOSE 8080
VOLUME ["/data"]
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/metatube-server"]
CMD ["--bind", "0.0.0.0", "--port", "8080", "--dsn", "/data/metatube.db"]
