# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
#
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.25-alpine AS builder

WORKDIR /go/src/github.com/sapcc/go-pmtud
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN go build -v -o /go-pmtud cmd/go-pmtud/main.go

FROM ubuntu:resolute@sha256:2260313b31c8c011cd2eebe728008efac1b3982be73eb71348ea2648d2c0e09b
LABEL source_repository="https://github.com/sapcc/go-pmtud"
RUN apt-get update && apt-get install -y \
    iptables iproute2 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /go-pmtud /go-pmtud
