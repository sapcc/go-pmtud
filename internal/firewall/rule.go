// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

import (
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const tableName = "pmtud"

// ifnamePad pads a network interface name to 16 bytes (null-terminated), matching
// the kernel's IFNAMSIZ representation used by nftables meta iifname comparisons.
func ifnamePad(name string) []byte {
	if len(name) > 15 {
		panic("interface name exceeds 15 bytes: " + name)
	}
	b := make([]byte, 16)
	copy(b, name+"\x00")
	return b
}

// buildNFTObjects constructs the nftables table, chain, and rule for the PMTUD
// NFLOG rule. No kernel I/O; safe to call in tests.
//
// Equivalent shell: iptables-nft -t raw -I PREROUTING -i <iifname> -p icmp
//	--icmp-type 3/4 -j NFLOG --nflog-group <nfGroup>
func buildNFTObjects(iifname string, nfGroup uint16) (*nftables.Table, *nftables.Chain, *nftables.Rule) {
	table := &nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   tableName,
	}
	chain := &nftables.Chain{
		Name:     "prerouting",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityRaw,
	}
	rule := &nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			// meta load iifname => reg 1
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			// cmp eq reg 1 <iifname padded to 16 bytes>
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifnamePad(iifname)},
			// meta load l4proto => reg 1
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			// cmp eq reg 1 IPPROTO_ICMP (1)
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_ICMP}},
			// payload load 1b @ transport header + 0 => reg 1  (ICMP type)
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 1},
			// cmp eq reg 1 3  (destination-unreachable)
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{3}},
			// payload load 1b @ transport header + 1 => reg 1  (ICMP code)
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 1, Len: 1},
			// cmp eq reg 1 4  (fragmentation needed)
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{4}},
			// log group <nfGroup>  (non-terminating NFLOG)
			&expr.Log{
				Key:   uint32(1 << unix.NFTA_LOG_GROUP),
				Group: nfGroup,
			},
		},
	}
	return table, chain, rule
}
