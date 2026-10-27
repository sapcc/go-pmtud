# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
#
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /go/src/github.com/sapcc/go-pmtud
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -v -o /go-pmtud cmd/go-pmtud/main.go

FROM alpine:latest@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
LABEL source_repository="https://github.com/sapcc/go-pmtud"
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go-pmtud /go-pmtud
ENTRYPOINT ["/go-pmtud"]
