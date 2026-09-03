# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
#
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

WORKDIR /go/src/github.com/sapcc/go-pmtud
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN go build -v -o /go-pmtud cmd/go-pmtud/main.go

FROM ubuntu:noble
LABEL source_repository="https://github.com/sapcc/go-pmtud"
RUN apt-get update && apt-get install -y \
    iptables iproute2 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /go-pmtud /go-pmtud
