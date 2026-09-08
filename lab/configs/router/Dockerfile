# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

FROM alpine:3.20

RUN apk add --no-cache \
    iproute2 \
    iptables \
    tcpdump \
    ethtool \
    bash

# Enable IP forwarding
RUN echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf

# Entry script that enables forwarding and keeps container running
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
