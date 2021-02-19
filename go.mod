module github.com/sapcc/go-pmtud

go 1.13

replace github.com/florianl/go-nflog/v2 v2.0.0 => github.com/sapcc/go-nflog/v2 v2.0.1

require (
	github.com/florianl/go-nflog/v2 v2.0.0
	github.com/mdlayher/ethernet v0.0.0-20190606142754-0394541c37b7
	github.com/mdlayher/raw v0.0.0-20191009151244-50f2db8cc065
	github.com/prometheus/client_golang v1.3.0
	github.com/spf13/pflag v1.0.5
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/net v0.0.0-20210119194325-5f4716e94777
	k8s.io/klog v1.0.0
)
