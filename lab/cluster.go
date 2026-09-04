// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/cluster"
)

func parseNodeLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func controlPlaneContainer(clusterName string) string {
	out, err := exec.Command("docker", "ps",
		"--filter", "label=io.x-k8s.kind.cluster="+clusterName,
		"--filter", "label=io.x-k8s.kind.role=control-plane",
		"--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return ""
	}
	if names := parseNodeLines(string(out)); len(names) > 0 {
		return names[0]
	}
	return ""
}

func createCluster(_ context.Context, name, configPath string) (*Cluster, error) {
	p := cluster.NewProvider()

	// Idempotent: check if exists
	existing, err := p.List()
	if err == nil {
		for _, c := range existing {
			if c == name {
				goto load_kubeconfig
			}
		}
	}

	// Create cluster
	if err := p.Create(name, cluster.CreateWithConfigFile(configPath)); err != nil {
		return nil, fmt.Errorf("kind create %s: %w", name, err)
	}

load_kubeconfig:
	// Get kubeconfig (do NOT merge into ~/.kube/config)
	kcfg, err := p.KubeConfig(name, false) // false = do not merge
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig %s: %w", name, err)
	}

	// Write to isolated temp file
	f, err := os.CreateTemp("", "kubeconfig-"+name+"-*")
	if err != nil {
		return nil, fmt.Errorf("create temp kubeconfig: %w", err)
	}
	defer f.Close()
	if _, err := f.Write([]byte(kcfg)); err != nil {
		return nil, fmt.Errorf("write kubeconfig: %w", err)
	}
	kcPath := f.Name()

	// Build client-go client from kubeconfig
	cfg, err := clientcmd.BuildConfigFromFlags("", kcPath)
	if err != nil {
		return nil, fmt.Errorf("build k8s config from kubeconfig: %w", err)
	}

	// Create controller-runtime client
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("create controller-runtime client: %w", err)
	}

	// Get worker nodes
	workers := workerContainers(name)

	return &Cluster{
		Name:           name,
		KubeconfigPath: kcPath,
		Client:         cl,
		Workers:        workers,
		ControlPlane:   controlPlaneContainer(name),
	}, nil
}

func deleteCluster(_ context.Context, name string) error {
	p := cluster.NewProvider()
	return p.Delete(name, "")
}

func workerContainers(clusterName string) []string {
	out, err := exec.Command("docker", "ps",
		"--filter", "label=io.x-k8s.kind.cluster="+clusterName,
		"--filter", "label=io.x-k8s.kind.role=worker",
		"--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return nil
	}
	return parseNodeLines(string(out))
}

func (c *Cluster) applyFile(ctx context.Context, path string) error {
	return run("kubectl", "--kubeconfig", c.KubeconfigPath, "apply", "-f", path)
}

func (c *Cluster) applyDaemonSet(ctx context.Context, path string, backend string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read daemonset: %w", err)
	}

	backendValue := backend
	stripRelay := false
	if backend == "legacy" {
		backendValue = "l2"
		stripRelay = true
	}

	if len(c.Workers) == 0 {
		return fmt.Errorf("no workers to detect eth0 MTU")
	}
	mtu, err := detectEth0MTU(c.Workers[0])
	if err != nil {
		return err
	}

	patched := patchDaemonSet(string(data), backendValue, mtu, stripRelay)

	f, err := os.CreateTemp("", "daemonset-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(patched); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return run("kubectl", "--kubeconfig", c.KubeconfigPath, "apply", "-n", "kube-system", "-f", f.Name())
}

// parseIfaceMTU parses the contents of /sys/class/net/<if>/mtu (e.g. "1500\n").
func parseIfaceMTU(out string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse iface mtu %q: %w", out, err)
	}
	return n, nil
}

// detectEth0MTU reads eth0's MTU on a node. All Kind nodes share the same eth0
// MTU, which is environment-dependent (65535 on Docker Desktop, 1500 on Linux).
func detectEth0MTU(node string) (int, error) {
	out, err := dockerExec(node, "cat", "/sys/class/net/eth0/mtu")
	if err != nil {
		return 0, fmt.Errorf("read eth0 mtu on %s: %w", node, err)
	}
	return parseIfaceMTU(out)
}

// patchDaemonSet substitutes the manifest placeholders. When stripRelayBackend
// is set (legacy), lines carrying --relay-backend are removed so the daemon
// defaults to l2 on its own.
func patchDaemonSet(data, backendValue string, ifaceMTU int, stripRelayBackend bool) string {
	patched := strings.ReplaceAll(data, "$(RELAY_BACKEND)", backendValue)
	patched = strings.ReplaceAll(patched, "$(IFACE_MTU)", strconv.Itoa(ifaceMTU))
	if stripRelayBackend {
		lines := strings.Split(patched, "\n")
		kept := lines[:0]
		for _, l := range lines {
			if !strings.Contains(l, "--relay-backend=") {
				kept = append(kept, l)
			}
		}
		patched = strings.Join(kept, "\n")
	}
	return patched
}

func (c *Cluster) waitRollout(ctx context.Context, ns, name string) error {
	return run("kubectl", "--kubeconfig", c.KubeconfigPath,
		"rollout", "status", "ds/"+name, "-n", ns, "--timeout=5m")
}
