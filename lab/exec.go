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

// containerIP returns the first docker-network IP of a container.
func containerIP(name string) (string, error) {
	out, err := exec.Command("docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w: %s", name, err, string(out))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("no IP for container %s", name)
	}
	return fields[0], nil
}
