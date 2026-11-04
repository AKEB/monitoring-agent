# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=local
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildvcs=false -trimpath \
	-ldflags "-s -w -X monitoring-agent/internal/buildinfo.Version=${VERSION}" \
	-o /monitoring-agent ./cmd/agent/

FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates tzdata

FROM scratch
COPY --from=runtime /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=runtime /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /monitoring-agent /monitoring-agent
ENTRYPOINT ["/monitoring-agent"]
