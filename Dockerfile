# syntax=docker/dockerfile:1

# metaX is a self-contained server. The engine, routes, and providers are all
# part of this repository; nothing is cloned or patched at build time.
ARG GO_VERSION=1.26.0
FROM golang:${GO_VERSION}-bookworm AS build

SHELL ["/bin/bash", "-c"]

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/metax

# Cache the module graph before copying the rest of the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG METAX_VERSION=
ARG METAX_COMMIT=
ARG METAX_GIT_REF=main
ARG METAX_GIT_URL=https://github.com/strange-dragon/metaX.git

RUN METAX_VERSION="$METAX_VERSION" \
    METAX_COMMIT="$METAX_COMMIT" \
    METAX_GIT_REF="$METAX_GIT_REF" \
    METAX_GIT_URL="$METAX_GIT_URL" \
    bash scripts/build-server.sh /out/metax-server

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 metax \
    && useradd --uid 1000 --gid metax --home-dir /data --shell /usr/sbin/nologin --no-create-home metax \
    && mkdir -p /data \
    && chown -R metax:metax /data

COPY --from=build /out/metax-server /usr/local/bin/metax-server
COPY LICENSE NOTICE.md /usr/share/licenses/metax/

LABEL org.opencontainers.image.title="metaX backend" \
      org.opencontainers.image.description="metaX metadata backend with native providers" \
      org.opencontainers.image.source="https://github.com/strange-dragon/metaX"

ENV GIN_MODE=release

USER metax
WORKDIR /data
EXPOSE 18080
VOLUME ["/data"]
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/metax-server"]
CMD ["--bind", "0.0.0.0", "--port", "18080", "--dsn", "/data/metax.db"]
