FROM golang:1.16.0-buster AS builder
ARG BUILD_VERSION='v1.2.3'
ENV LISTEN_PORT=9101
WORKDIR /opt/keepup/
COPY go.mod ./
COPY go.sum .
COPY src ./src
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.buildVersion=${BUILD_VERSION}" -a -installsuffix cgo -o keepup src/main.go

FROM debian:buster
WORKDIR /opt/keepup/
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
  && rm -rf /var/cache/apt/archives/*
COPY --from=builder /opt/keepup/keepup .

EXPOSE ${LISTEN_PORT}
CMD ["/opt/keepup/keepup"]
