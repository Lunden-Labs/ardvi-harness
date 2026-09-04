# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
FROM golang:1.26.8-alpine3.23@sha256:33ce311e5eecedee48ec1b84419c1306e9fbd71009f0d5c3f2a6904b579c1ecc AS build
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY .harness/mcp/go.mod .harness/mcp/go.sum ./
RUN go mod download
COPY .harness/mcp/ ./
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/ardvi ./cmd/ardvi-mcp

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS skills
RUN apk add --no-cache bash git python3
COPY .harness /src/.harness
RUN /src/.harness/scripts/prepare_image_skills.sh /rootfs

FROM scratch
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.source="https://github.com/Lunden-Labs/ardvi-harness" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"
COPY --from=build /out/ardvi /usr/local/bin/ardvi
COPY --from=skills --chown=65532:65532 /rootfs/opt/ardvi /opt/ardvi
COPY --from=skills --chown=65532:65532 /rootfs/var/lib/ardvi /var/lib/ardvi
USER 65532:65532
ENV ARDVI_CONTAINER=1
EXPOSE 8765
ENTRYPOINT ["/usr/local/bin/ardvi"]
CMD ["serve", "--listen", "0.0.0.0:8765", "--allow-non-loopback", "--data", "/var/lib/ardvi", "--catalog", "/opt/ardvi/catalog.json"]
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD ["/usr/local/bin/ardvi", "healthcheck"]
