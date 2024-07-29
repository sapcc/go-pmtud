// Copyright 2024 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	goflag "flag"
	"fmt"
	"math/rand"
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

	conf "github.com/sapcc/go-pmtud/internal/config"
	metr "github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/nflog"
	"github.com/sapcc/go-pmtud/internal/node"
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
	rootCmd.PersistentFlags().StringSliceVar(&cfg.InterfaceNames, "iface_names", nil, "Replication interface names to work on")
	rootCmd.PersistentFlags().StringVar(&cfg.NodeName, "nodename", "", "Node hostname")
	rootCmd.PersistentFlags().IntVar(&cfg.InterfaceMtu, "iface_mtu", 1500, "MTU size that replication interface should have")
	// rootCmd.PersistentFlags().StringSliceVar(&cfg.Peers, "peers", nil, "Resend ICMP frag-needed packets to this peer list (comma separated)")
	rootCmd.PersistentFlags().StringVar(&cfg.MetricsPort, "metrics_port", ":30040", "Port for Prometheus metrics")
	rootCmd.PersistentFlags().StringVar(&cfg.HealthPort, "health_port", ":30041", "Port for healthz")
	rootCmd.PersistentFlags().Uint16Var(&cfg.NfGroup, "nflog_group", 33, "NFLOG group")
	rootCmd.PersistentFlags().IntVar(&cfg.TimeToLive, "ttl", 1, "TTL for resent packets")
	rootCmd.PersistentFlags().IntVar(&cfg.ArpCacheTimeoutMinutes, "node-timeout-minutes", 5, "Timeout in minutes for node arp entry")
	rootCmd.PersistentFlags().IntVar(&cfg.ArpRequestTimeoutSeconds, "arp-timeout-seconds", 1, "Timeout in seconds for node arp request")
	rootCmd.PersistentFlags().StringVar(&cfg.KubeContext, "kube_context", "", "kube-context to use")
	rootCmd.PersistentFlags().AddGoFlagSet(goflag.CommandLine)
	err := viper.BindPFlags(rootCmd.PersistentFlags())
	if err != nil {
		os.Exit(1)
	}

	rsource := rand.NewSource(time.Now().UnixNano())
	rng := rand.New(rsource) //nolint:gosec // Ignoring G404: Use of weak random number generator (math/rand instead of crypto/rand)
	cfg.RandDelay = rng.Intn(1000) + 1000

	metrics.Registry.MustRegister(metr.SentError, metr.Error, metr.ArpResolveError, metr.SentPacketsPeer, metr.SentPackets, metr.RecvPackets, metr.CallbackDuration)
	cfg.PeerList = make(map[string]conf.PeerEntry)
}

func preRunRootCmd(cmd *cobra.Command, args []string) error {
	log := zap.New(func(o *zap.Options) {
		o.Development = true
	}).WithName("preRunRoot")
	err := util.GetReplicationInterface(&cfg, log)
	if err != nil {
		return err
	}
	err = util.GetDefaultInterface(&cfg, log)
	if err != nil {
		return err
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

	// add nfLog controller
	nfc := nflog.Controller{
		Log: log.WithName("nfLog-controller"),
		Cfg: &cfg,
	}
	err = mgr.Add(&nfc)
	if err != nil {
		log.Error(err, "error adding nfLog-controller")
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
