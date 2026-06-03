FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/dns-swr ./cmd/dns-swr

FROM alpine:3.22

RUN apk add --no-cache libcap \
    && adduser -D -H -s /sbin/nologin dns-swr
WORKDIR /app
COPY --from=build /out/dns-swr /usr/local/bin/dns-swr
COPY configs/config.example.yaml /app/config.yaml
RUN setcap 'cap_net_bind_service=+ep' /usr/local/bin/dns-swr
USER dns-swr

EXPOSE 53/udp 53/tcp 8053/tcp
ENTRYPOINT ["/usr/local/bin/dns-swr"]
CMD ["-config", "/app/config.yaml"]
