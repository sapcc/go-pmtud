module github.com/sapcc/go-pmtud

go 1.13

replace github.com/florianl/go-nflog/v2 v2.0.0 => github.com/sapcc/go-nflog/v2 v2.0.1

replace github.com/mdlayher/arp v0.0.0-20191213142603-f72070a231fc => github.com/sapcc/arp v0.0.0-20210323090929-4fa8e70001f0

require (
	github.com/florianl/go-nflog/v2 v2.0.1
	github.com/go-logr/logr v0.4.0
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118
	github.com/mdlayher/raw v0.1.0
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.5.0
	github.com/spf13/viper v1.7.0
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/net v0.0.0-20220811182439-13a9a731de15
	k8s.io/api v0.20.2
	k8s.io/client-go v0.20.2
	sigs.k8s.io/controller-runtime v0.8.3
)
