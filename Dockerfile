# gcr.io/distroless/static:nonroot includes CA roots and defaults to UID 65532.
# Pinned multi-platform index; shared by local smoke and both release images.
FROM gcr.io/distroless/static@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

# GoReleaser v2 places each binary under its target platform directory.
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/dox /usr/bin/dox

ENTRYPOINT ["/usr/bin/dox"]
CMD ["--help"]
