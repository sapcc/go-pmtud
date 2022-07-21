module github.com/sapcc/go-pmtud

go 1.13

replace github.com/florianl/go-nflog/v2 v2.0.0 => github.com/sapcc/go-nflog/v2 v2.0.1

replace github.com/mdlayher/arp v0.0.0-20191213142603-f72070a231fc => github.com/sapcc/arp v0.0.0-20210323090929-4fa8e70001f0

require (
	github.com/florianl/go-nflog/v2 v2.0.0
	github.com/go-logr/logr v0.3.0
	github.com/mdlayher/arp v0.0.0-20191213142603-f72070a231fc
	github.com/mdlayher/ethernet v0.0.0-20190606142754-0394541c37b7
	github.com/mdlayher/raw v0.0.0-20191009151244-50f2db8cc065
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.5.0
	github.com/spf13/viper v1.7.0
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/net v0.0.0-20210119194325-5f4716e94777
	k8s.io/api v0.20.2
	k8s.io/client-go v0.20.2
	sigs.k8s.io/controller-runtime v0.8.3
)
