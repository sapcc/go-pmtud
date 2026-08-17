// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func dockerExec(container string, args ...string) (string, error) {
	fullArgs := append([]string{"exec", container}, args...)
	out, err := exec.Command("docker", fullArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker exec %s: %w: %s", container, err, string(out))
	}
	return string(out), nil
}

func ifaceByIP(container, ip string) (string, error) {
	out, err := dockerExec(container, "ip", "-o", "addr", "show")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.Contains(fields[3], ip) {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("interface with %s not found on %s", ip, container)
}
