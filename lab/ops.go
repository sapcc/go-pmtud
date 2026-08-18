// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (l *Lab) GenerateTraffic(ctx context.Context) error {
	if len(l.ClusterA.Workers) == 0 {
		return fmt.Errorf("no cluster-a workers")
	}
	worker := l.ClusterA.Workers[0]
	// POST 2100 ASCII bytes so curl sends a body > 1500 bytes through the MTU bottleneck.
	// null bytes from /dev/zero cause curl -d to send an empty body; use yes+head for ASCII.
	_, err := dockerExec(worker, "sh", "-c",
		fmt.Sprintf("yes A | head -c 2100 | curl -s -o /dev/null --data-binary @- http://%s:30080/echo", l.DestIP))
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
	dockerExec(node, "ip", "route", "flush", "cache") // best-effort: no-op on kernels without route cache
	return nil
}

type ICMPCapture struct {
	Count int
	Done  chan struct{}
}

func (l *Lab) CaptureICMPAsync(ctx context.Context, dur time.Duration) *ICMPCapture {
	cap := &ICMPCapture{Count: 0, Done: make(chan struct{})}
	go func() {
		defer close(cap.Done)
		cmd := exec.Command("docker", "exec", l.Router,
			"sh", "-c", "tcpdump -i any -n -l 'icmp[0]=3 and icmp[1]=4' 2>/dev/null")
		pipe, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			time.Sleep(dur)
			return
		}
		scanned := make(chan struct{})
		go func() {
			defer close(scanned)
			scanner := bufio.NewScanner(pipe)
			for scanner.Scan() {
				cap.Count++ // benign race: only grows, Eventually polls after writes settle
			}
		}()
		time.Sleep(dur)
		cmd.Process.Kill()
		<-scanned
	}()
	return cap
}
