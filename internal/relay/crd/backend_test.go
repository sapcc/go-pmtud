// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package crd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/sapcc/go-pmtud/api/v1alpha1"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/relay"
)

var (
	envCfg    *rest.Config
	envClient client.Client
	envScheme *runtime.Scheme
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		fmt.Fprintln(os.Stderr, "skipping envtest: KUBEBUILDER_ASSETS not set")
		os.Exit(0)
	}

	envScheme = runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(envScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add clientgo scheme: %v\n", err)
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(envScheme); err != nil {
		fmt.Fprintf(os.Stderr, "add v1alpha1 scheme: %v\n", err)
		os.Exit(1)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths: []string{"../../../crd"},
	}

	var err error
	envCfg, err = env.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envtest start: %v\n", err)
		os.Exit(1)
	}

	envClient, err = client.New(envCfg, client.Options{Scheme: envScheme})
	if err != nil {
		if stopErr := env.Stop(); stopErr != nil {
			fmt.Fprintf(os.Stderr, "envtest stop: %v\n", stopErr)
		}
		fmt.Fprintf(os.Stderr, "create client: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	if stopErr := env.Stop(); stopErr != nil {
		fmt.Fprintf(os.Stderr, "envtest stop: %v\n", stopErr)
	}
	os.Exit(code)
}

// newTestNamespace creates a fresh namespace for a test and deletes it on cleanup.
func newTestNamespace(t *testing.T) string {
	t.Helper()
	name := strings.NewReplacer("_", "-", "/", "-").Replace(strings.ToLower(t.Name()))
	if len(name) > 63 {
		name = name[:63]
	}
	if err := envClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
		t.Fatalf("create namespace %q: %v", name, err)
	}
	t.Cleanup(func() {
		if err := envClient.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
			t.Logf("cleanup namespace %q: %v", name, err)
		}
	})
	return name
}

// newStartedCache creates a cache tied to ctx and waits for its initial sync.
func newStartedCache(ctx context.Context, t *testing.T) ctrlcache.Cache {
	t.Helper()
	c, err := ctrlcache.New(envCfg, ctrlcache.Options{Scheme: envScheme})
	if err != nil {
		t.Fatalf("create cache: %v", err)
	}
	go func() {
		if err := c.Start(ctx); err != nil && ctx.Err() == nil {
			t.Errorf("cache: %v", err)
		}
	}()
	if !c.WaitForCacheSync(ctx) {
		t.Fatal("cache did not sync")
	}
	return c
}

func TestRelayObjectNameStable(t *testing.T) {
	p := []byte{1, 2, 3, 4}
	a := relayObjectName("node-a", p)
	b := relayObjectName("node-a", p)
	if a != b {
		t.Fatalf("name not deterministic: %q vs %q", a, b)
	}
	if relayObjectName("node-b", p) == a {
		t.Fatal("different source nodes must yield different names")
	}
	// <node>--<32 hex chars> (16 bytes of sha256)
	if len(a) != len("node-a")+2+32 {
		t.Fatalf("unexpected name shape: %q", a)
	}
}

func TestBackendSend_CreatesObject(t *testing.T) {
	ns := newTestNamespace(t)
	b := &backend{
		cfg:    &config.Config{NodeName: "node-a"},
		log:    logr.Discard(),
		client: envClient,
		ns:     ns,
		ttl:    time.Minute,
		gcTick: time.Minute,
	}

	payload := []byte{10, 20, 30}
	if err := b.Send(context.Background(), relay.RelayPacket{SrcNode: "node-a", Payload: payload}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var list v1alpha1.PMTUNodeRelayList
	if err := envClient.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 object, got %d", len(list.Items))
	}
	got := list.Items[0]
	if got.Spec.SourceNode != "node-a" {
		t.Errorf("SourceNode: got %q, want %q", got.Spec.SourceNode, "node-a")
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Spec.Payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if string(decoded) != string(payload) {
		t.Errorf("Payload: got %v, want %v", decoded, payload)
	}
	if got.Spec.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should not be zero")
	}
}

func TestBackendSend_Idempotent(t *testing.T) {
	ns := newTestNamespace(t)
	b := &backend{
		cfg:    &config.Config{NodeName: "node-a"},
		log:    logr.Discard(),
		client: envClient,
		ns:     ns,
		ttl:    time.Minute,
		gcTick: time.Minute,
	}

	pkt := relay.RelayPacket{SrcNode: "node-a", Payload: []byte{1, 2, 3}}
	if err := b.Send(context.Background(), pkt); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	if err := b.Send(context.Background(), pkt); err != nil {
		t.Fatalf("second Send (idempotent): %v", err)
	}

	var list v1alpha1.PMTUNodeRelayList
	if err := envClient.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected 1 object after duplicate send, got %d", len(list.Items))
	}
}

func TestBackendStart_InjectsFromOtherNode(t *testing.T) {
	ns := newTestNamespace(t)
	ctx := t.Context()

	b := &backend{
		cfg:    &config.Config{NodeName: "node-self"},
		log:    logr.Discard(),
		client: envClient,
		cache:  newStartedCache(ctx, t),
		ns:     ns,
		ttl:    time.Minute,
		gcTick: time.Minute,
	}

	injected := make(chan []byte, 1)
	go func() {
		if err := b.Start(ctx, func(payload []byte) error {
			injected <- payload
			return nil
		}); err != nil {
			t.Errorf("Start: %v", err)
		}
	}()
	// Allow the informer handler to register before creating objects.
	time.Sleep(200 * time.Millisecond)

	payload := []byte{1, 2, 3, 4, 5}
	obj := &v1alpha1.PMTUNodeRelay{
		ObjectMeta: metav1.ObjectMeta{Name: "relay-from-other", Namespace: ns},
		Spec: v1alpha1.PMTUNodeRelaySpec{
			SourceNode: "node-other",
			Payload:    base64.StdEncoding.EncodeToString(payload),
			ExpiresAt:  metav1.NewTime(time.Now().Add(time.Minute)),
		},
	}
	if err := envClient.Create(ctx, obj); err != nil {
		t.Fatalf("create relay object: %v", err)
	}

	select {
	case got := <-injected:
		if string(got) != string(payload) {
			t.Errorf("payload: got %v, want %v", got, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for inject")
	}
}

func TestBackendStart_IgnoresSameNode(t *testing.T) {
	ns := newTestNamespace(t)
	ctx := t.Context()

	b := &backend{
		cfg:    &config.Config{NodeName: "node-self"},
		log:    logr.Discard(),
		client: envClient,
		cache:  newStartedCache(ctx, t),
		ns:     ns,
		ttl:    time.Minute,
		gcTick: time.Minute,
	}

	injected := make(chan []byte, 1)
	go func() {
		if err := b.Start(ctx, func(payload []byte) error {
			injected <- payload
			return nil
		}); err != nil {
			t.Errorf("Start: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond)

	obj := &v1alpha1.PMTUNodeRelay{
		ObjectMeta: metav1.ObjectMeta{Name: "relay-from-self", Namespace: ns},
		Spec: v1alpha1.PMTUNodeRelaySpec{
			SourceNode: "node-self",
			Payload:    base64.StdEncoding.EncodeToString([]byte{9, 8, 7}),
			ExpiresAt:  metav1.NewTime(time.Now().Add(time.Minute)),
		},
	}
	if err := envClient.Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	select {
	case p := <-injected:
		t.Errorf("should not inject own packet, got %v", p)
	case <-time.After(500 * time.Millisecond):
		// expected: no inject
	}
}

func TestBackendGCExpired_DeletesExpiredObjects(t *testing.T) {
	ns := newTestNamespace(t)
	ctx := context.Background()

	b := &backend{
		cfg:    &config.Config{NodeName: "node-self"},
		log:    logr.Discard(),
		client: envClient,
		ns:     ns,
		gcTick: time.Minute,
	}

	create := func(name, srcNode string, expiresAt time.Time) {
		t.Helper()
		obj := &v1alpha1.PMTUNodeRelay{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: v1alpha1.PMTUNodeRelaySpec{
				SourceNode: srcNode,
				Payload:    base64.StdEncoding.EncodeToString([]byte{1}),
				ExpiresAt:  metav1.NewTime(expiresAt),
			},
		}
		if err := envClient.Create(ctx, obj); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
	}

	create("expired-own", "node-self", time.Now().Add(-time.Minute))
	create("live-own", "node-self", time.Now().Add(time.Hour))
	create("expired-foreign", "node-other", time.Now().Add(-time.Minute)) // not this node's to GC

	b.gcExpired(ctx)

	var obj v1alpha1.PMTUNodeRelay
	key := func(name string) client.ObjectKey { return client.ObjectKey{Name: name, Namespace: ns} }

	if err := envClient.Get(ctx, key("expired-own"), &obj); !apierrors.IsNotFound(err) {
		t.Errorf("expired-own should be deleted, got err: %v", err)
	}
	if err := envClient.Get(ctx, key("live-own"), &obj); err != nil {
		t.Errorf("live-own should still exist: %v", err)
	}
	if err := envClient.Get(ctx, key("expired-foreign"), &obj); err != nil {
		t.Errorf("expired-foreign should still exist (not this node's): %v", err)
	}
}
