FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ENV TARGETOS=$TARGETOS
ENV TARGETARCH=$TARGETARCH

RUN apk add --no-cache just git

WORKDIR /src
COPY . .
RUN just build

FROM alpine:latest

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 aeroflare

COPY --from=builder /src/out/aeroflare /usr/local/bin/aeroflare

USER aeroflare

ENV NIXCACHE_LISTEN=0.0.0.0
EXPOSE 8080

ENTRYPOINT ["aeroflare"]
CMD ["proxy"]
