// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	goflag "flag"
	"fmt"
	"net"
	"os"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/sapcc/go-pmtud/api/v1alpha1"
	conf "github.com/sapcc/go-pmtud/internal/config"
	metr "github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/nflog"
	"github.com/sapcc/go-pmtud/internal/node"
	"github.com/sapcc/go-pmtud/internal/relay"
	"github.com/sapcc/go-pmtud/internal/relay/crd"
	"github.com/sapcc/go-pmtud/internal/relay/udp"
	"github.com/sapcc/go-pmtud/internal/util"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var rootCmd = &cobra.Command{
	Use:     "pmtud",
	Short:   "",
	Long:    "",
	RunE:    runRootCmd,
	PreRunE: preRunRootCmd,
}
var cfg = conf.Config{}

func init() {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("PMTUD")
	rootCmd.PersistentFlags().StringVar(&cfg.NodeName, "nodename", "", "Node hostname")
	rootCmd.PersistentFlags().StringVar(&cfg.MetricsPort, "metrics_port", ":30040", "Port for Prometheus metrics")
	rootCmd.PersistentFlags().StringVar(&cfg.HealthPort, "health_port", ":30041", "Port for healthz")
	rootCmd.PersistentFlags().Uint16Var(&cfg.NfGroup, "nflog_group", 33, "NFLOG group")
	rootCmd.PersistentFlags().IntVar(&cfg.TimeToLive, "ttl", 1, "TTL for resent packets")
	rootCmd.PersistentFlags().IntVar(&cfg.ReplicationPort, "replication-port", 4390, "UDP port for ICMP packet replication between nodes. Only used when relay-backend=udp")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.IgnoreNetworksRaw, "ignore-networks", nil, "Do not resend ICMP frag-needed packets originated from specified networks (comma-separated CIDRs)")
	rootCmd.PersistentFlags().StringVar(&cfg.KubeContext, "kube_context", "", "kube-context to use")
	cfg.RelayBackend = conf.BackendCRD
	rootCmd.PersistentFlags().Var(&cfg.RelayBackend, "relay-backend", "Relay backend: udp or crd")
	rootCmd.PersistentFlags().StringVar(&cfg.RelayNamespace, "relay-namespace", "", "Namespace for CRD relay objects (defaults to POD_NAMESPACE env var)")
	rootCmd.PersistentFlags().DurationVar(&cfg.RelayGCInterval, "relay-gc-interval", 60*time.Second, "CRD relay garbage collection interval")
	rootCmd.PersistentFlags().AddGoFlagSet(goflag.CommandLine)
	err := viper.BindPFlags(rootCmd.PersistentFlags())
	if err != nil {
		os.Exit(1)
	}

	metrics.Registry.MustRegister(metr.SentError, metr.Error, metr.SentPacketsPeer, metr.SentPackets, metr.RecvPackets, metr.CallbackDuration)
	cfg.PeerList = make(map[string]string)
}

func preRunRootCmd(cmd *cobra.Command, args []string) error {
	log := zap.New(func(o *zap.Options) {
		o.Development = true
	}).WithName("preRunRoot")
	err := util.GetDefaultInterface(&cfg, log)
	if err != nil {
		return err
	}
	// Parse ignore-networks CIDRs
	for _, cidr := range cfg.IgnoreNetworksRaw {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid ignore-network CIDR %q: %w", cidr, err)
		}
		cfg.IgnoreNetworks = append(cfg.IgnoreNetworks, ipNet)
	}
	// Validate replication port flag is only set if relay backend is udp
	if cfg.RelayBackend != conf.BackendUDP && cmd.PersistentFlags().Changed("replication-port") {
		return errors.New("--replication-port is only valid with --relay-backend=udp")
	}

	// Resolve relay namespace from flag or POD_NAMESPACE env var
	if cfg.RelayBackend == conf.BackendCRD {
		if cfg.RelayNamespace == "" {
			cfg.RelayNamespace = os.Getenv("POD_NAMESPACE")
		}
		if cfg.RelayNamespace == "" {
			return errors.New("relay backend is 'crd' but no namespace could be resolved: set --relay-namespace or POD_NAMESPACE env var")
		}
	}
	return nil
}

func runRootCmd(cmd *cobra.Command, args []string) error {
	log := zap.New(func(o *zap.Options) {
		o.Development = true
	}).WithName("runRoot")
	ctrl.SetLogger(log)
	managerOpts := manager.Options{
		Metrics:                metricsserver.Options{BindAddress: cfg.MetricsPort},
		HealthProbeBindAddress: cfg.HealthPort,
	}
	restConfig, err := config.GetConfigWithContext(cfg.KubeContext)
	if err != nil {
		log.Error(err, "error getting kube config. Exiting.")
		os.Exit(1)
	}
	mgr, err := manager.New(restConfig, managerOpts)
	if err != nil {
		log.Error(err, "error creating manager. Exiting.")
		os.Exit(1)
	}

	// register v1alpha1 scheme
	if err := v1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "error registering v1alpha1 scheme")
		return err
	}

	// add node-controller
	c, err := controller.New("node-controller", mgr, controller.Options{
		Reconciler: &node.Reconciler{
			Log:    mgr.GetLogger().WithName("node-controller"),
			Client: mgr.GetClient(),
			Cfg:    &cfg,
		},
	})
	if err != nil {
		log.Error(err, "error creating node-controller")
		return err
	}
	err = c.Watch(source.Kind(mgr.GetCache(), &corev1.Node{}, &handler.TypedEnqueueRequestForObject[*corev1.Node]{}))
	if err != nil {
		log.Error(err, "error watching nodes")
		return err
	}

	// build relay backend
	deps := relay.Deps{
		Cfg:    &cfg,
		Log:    log.WithName("relay"),
		Client: mgr.GetClient(),
		Cache:  mgr.GetCache(),
	}
	var relayBackend relay.Relay
	switch cfg.RelayBackend {
	case conf.BackendUDP:
		relayBackend, err = udp.New(deps)
	case conf.BackendCRD:
		relayBackend, err = crd.New(deps)
	default:
		return fmt.Errorf("unknown relay backend %q", cfg.RelayBackend)
	}
	if err != nil {
		log.Error(err, "error creating relay backend")
		return err
	}

	// add nfLog controller
	nfc := nflog.Controller{
		Log:   log.WithName("nfLog-controller"),
		Cfg:   &cfg,
		Relay: relayBackend,
	}
	err = mgr.Add(&nfc)
	if err != nil {
		log.Error(err, "error adding nfLog-controller")
		return err
	}

	// add relay runnable
	err = mgr.Add(&relay.Runnable{
		Backend: relayBackend,
		Log:     log.WithName("relay-runnable"),
	})
	if err != nil {
		log.Error(err, "error adding relay-runnable")
		return err
	}

	err = mgr.Start(signals.SetupSignalHandler())
	if err != nil {
		log.Error(err, "error starting manager")
		return err
	}
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
