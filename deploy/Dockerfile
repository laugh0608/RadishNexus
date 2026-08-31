ARG GO_IMAGE="golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57"
ARG NODE_IMAGE="node:24.16.0-alpine3.23@sha256:2bdb65ed1dab192432bc31c95f94155ca5ad7fc1392fb7eb7526ab682fa5bf14"
ARG ALPINE_IMAGE="alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40"
ARG POSTGRES_IMAGE="postgres:17.10-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"

FROM ${NODE_IMAGE} AS web-build
WORKDIR /build/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS go-build
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download && go mod verify
COPY server/ ./
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/radishnexus ./cmd/radishnexus \
    && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/nexus-migrate ./cmd/nexus-migrate \
    && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/nexus-bootstrap ./cmd/nexus-bootstrap \
    && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/nexus-backup ./cmd/nexus-backup \
    && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /out/nexus-restore ./cmd/nexus-restore

FROM ${ALPINE_IMAGE} AS server
RUN addgroup -S -g 10001 radishnexus \
    && adduser -S -D -H -u 10001 -G radishnexus radishnexus
WORKDIR /srv/radishnexus
COPY --from=go-build /out/radishnexus /usr/local/bin/radishnexus
COPY --from=web-build --chown=10001:10001 /build/web/dist ./web
COPY LICENSE /usr/share/licenses/RadishNexus/LICENSE
ENV RADISHNEXUS_WEB_ROOT=/srv/radishnexus/web
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/radishnexus"]

FROM ${POSTGRES_IMAGE} AS operations
COPY --from=go-build /out/nexus-migrate /usr/local/bin/nexus-migrate
COPY --from=go-build /out/nexus-bootstrap /usr/local/bin/nexus-bootstrap
COPY --from=go-build /out/nexus-backup /usr/local/bin/nexus-backup
COPY --from=go-build /out/nexus-restore /usr/local/bin/nexus-restore
COPY LICENSE /usr/share/licenses/RadishNexus/LICENSE
USER postgres
ENTRYPOINT []
CMD []
