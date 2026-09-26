# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Download modules first so the layer is cached across source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/smsmail ./cmd/smsmail

# ---- runtime stage ---------------------------------------------------------
FROM alpine:3.21

# Listeners default to :1025 (SMTP), :8025 (web UI) and :8080 (mock SMS API),
# which bind every interface inside the container.
RUN adduser -D -u 10001 smsmail
USER smsmail
WORKDIR /home/smsmail

COPY --from=build /out/smsmail /usr/local/bin/smsmail

EXPOSE 1025 8025 8080

ENTRYPOINT ["smsmail"]
