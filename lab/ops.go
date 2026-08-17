// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (l *Lab) GenerateTraffic(ctx context.Context) error {
	if len(l.ClusterA.Workers) == 0 {
		return fmt.Errorf("no cluster-a workers")
	}
	worker := l.ClusterA.Workers[0]

	// curl from cluster-a worker to podinfo on cluster-b (1MB file)
	_, err := dockerExec(worker, "curl", "-s", "-o", "/dev/null",
		fmt.Sprintf("http://%s:30080/testfile", l.DestIP))
	return err
}

func (l *Lab) PMTUTo(node, dst string) (int, error) {
	out, err := dockerExec(node, "ip", "route", "get", dst)
	if err != nil {
		return 0, err
	}

	// parse "... mtu 1500 ..." from output
	fields := strings.Fields(out)
	for i, field := range fields {
		if field == "mtu" {
			// next field is the MTU
			if i+1 < len(fields) {
				if m, err := strconv.Atoi(fields[i+1]); err == nil {
					return m, nil
				}
			}
		}
		if strings.HasPrefix(field, "mtu") {
			// "mtu1500" format (no space)
			mtuStr := strings.TrimPrefix(field, "mtu")
			if m, err := strconv.Atoi(mtuStr); err == nil {
				return m, nil
			}
		}
	}
	return 0, fmt.Errorf("no MTU in route output: %s", out)
}

func (l *Lab) FlushRouteCache(node string) error {
	_, err := dockerExec(node, "ip", "route", "flush", "cache")
	return err
}

type ICMPCapture struct {
	Count int
	Done  chan struct{}
}

func (l *Lab) CaptureICMPAsync(ctx context.Context, dur time.Duration) *ICMPCapture {
	cap := &ICMPCapture{Count: 0, Done: make(chan struct{})}
	go func() {
		// TODO: start tcpdump on router, count packets matching ICMP type 3/4
		time.Sleep(dur)
		close(cap.Done)
	}()
	return cap
}
