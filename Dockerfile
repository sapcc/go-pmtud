# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
#
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.26-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /go/src/github.com/sapcc/go-pmtud
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -v -o /go-pmtud cmd/go-pmtud/main.go

FROM alpine:latest AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
LABEL source_repository="https://github.com/sapcc/go-pmtud"
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go-pmtud /go-pmtud
ENTRYPOINT ["/go-pmtud"]
