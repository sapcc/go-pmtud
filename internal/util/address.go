package util

import (
	"net"
)

// CalcSrcDst counts inner packet IPv4 IPs with bytes due to missing ICMP 3/4 parsing library
func CalcSrcDst(b []byte) (srcIP net.IP, dstIP net.IP, err error) {
	src := b[40:44]
	dst := b[44:48]

	srcIP = src
	dstIP = dst

	// validate if parsed IPs are valid IPv4 addresses
	if (net.ParseIP(srcIP.String()) == nil) || (net.ParseIP(dstIP.String()) == nil) {
		return nil, nil, err
	} else {
		return srcIP, dstIP, nil
	}
}
