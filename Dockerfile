FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go test ./... && go vet ./...
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/singbox-sub .

FROM ghcr.io/sagernet/sing-box:v1.14.4 AS singbox

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && mkdir -m 700 /data
COPY --from=singbox /usr/local/bin/sing-box /usr/local/bin/sing-box
COPY --from=builder /out/singbox-sub /usr/local/bin/singbox-sub
EXPOSE 24443/tcp 8080/tcp
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/singbox-sub"]
