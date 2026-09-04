// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"encoding/json"
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

const kindNetwork = "kind"

// ipFromNetworksJSON extracts a container's IP on a named docker network from
// the JSON emitted by `docker inspect -f '{{json .NetworkSettings.Networks}}'`.
func ipFromNetworksJSON(jsonStr, network string) (string, error) {
	var nets map[string]struct {
		IPAddress string `json:"IPAddress"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &nets); err != nil {
		return "", fmt.Errorf("parse networks json: %w", err)
	}
	n, ok := nets[network]
	if !ok {
		return "", fmt.Errorf("container not on network %q", network)
	}
	if n.IPAddress == "" {
		return "", fmt.Errorf("no IP for container on network %q", network)
	}
	return n.IPAddress, nil
}

// containerIPOnNetwork returns a container's IP on a specific docker network.
func containerIPOnNetwork(name, network string) (string, error) {
	out, err := exec.Command("docker", "inspect", "-f",
		"{{json .NetworkSettings.Networks}}", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w: %s", name, err, string(out))
	}
	ip, err := ipFromNetworksJSON(strings.TrimSpace(string(out)), network)
	if err != nil {
		return "", fmt.Errorf("container %s: %w", name, err)
	}
	return ip, nil
}

// containerIP returns a node's kind-network IP (its Kubernetes InternalIP / eth0).
func containerIP(name string) (string, error) {
	return containerIPOnNetwork(name, kindNetwork)
}
