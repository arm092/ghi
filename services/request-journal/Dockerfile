# syntax=docker/dockerfile:1
FROM alpine:3.22 AS build
ARG TARGETARCH
ARG GHI_VERSION=1.0.0
ENV GHI_INSTALL_DIR=/usr/local/bin GHI_NO_PATH=1 CGO_ENABLED=0
RUN apk add --no-cache ca-certificates curl git
WORKDIR /toolchain
RUN set -eu; \
    archive="ghi_v${GHI_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    base="https://github.com/arm092/ghi/releases/download/v${GHI_VERSION}"; \
    curl --fail --location --retry 3 "$base/$archive" -o "$archive"; \
    curl --fail --location --retry 3 "$base/checksums.txt" -o checksums.txt; \
    awk -v name="$archive" '$2 == name { print }' checksums.txt > archive.sha256; \
    test -s archive.sha256; \
    sha256sum -c archive.sha256; \
    tar -xzf "$archive"; \
    sh install.sh; \
    ghi --version; mojave --version
WORKDIR /src
COPY mojave.json mojave.lock ./
RUN mojave install
COPY . .
RUN ghi test . && ghi build -o /out/journal .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 journal \
    && adduser -D -H -u 10001 -G journal journal \
    && mkdir -p /app /data && chown journal:journal /data
WORKDIR /app
COPY --from=build /out/journal /app/journal
COPY migrations /app/migrations
ENV JOURNAL_ADDR=0.0.0.0:8080 \
    JOURNAL_DB=/data/journal.db \
    JOURNAL_MIGRATIONS=/app/migrations \
    JOURNAL_SHUTDOWN_TIMEOUT=5s
USER 10001:10001
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/app/journal"]
