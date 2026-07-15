FROM golang:1.26-alpine AS builder

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
EXPOSE 37515

ENTRYPOINT ["aeroflare"]
CMD ["proxy"]
