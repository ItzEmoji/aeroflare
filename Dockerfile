FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ENV TARGETOS=$TARGETOS
ENV TARGETARCH=$TARGETARCH

RUN apk add --no-cache make git

WORKDIR /src
COPY . .
RUN make build

FROM alpine:latest

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 aeroflare

COPY --from=builder /src/out/aeroflare /usr/local/bin/aeroflare

USER aeroflare

ENV NIXCACHE_LISTEN=0.0.0.0
ENV NIXCACHE_PORT=8080
EXPOSE 8080

ENTRYPOINT ["aeroflare"]
CMD ["proxy"]
