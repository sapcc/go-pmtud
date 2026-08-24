// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

import (
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

func TestBuildNFTObjects(t *testing.T) {
	table, chain, rule := buildNFTObjects("eth0", 33)

	// table
	if table.Name != "pmtud" {
		t.Errorf("table name: got %q, want %q", table.Name, "pmtud")
	}
	if table.Family != nftables.TableFamilyIPv4 {
		t.Errorf("table family: got %v, want TableFamilyIPv4", table.Family)
	}

	// chain
	if chain.Name != "prerouting" {
		t.Errorf("chain name: got %q, want %q", chain.Name, "prerouting")
	}
	if chain.Type != nftables.ChainTypeFilter {
		t.Errorf("chain type: got %v, want ChainTypeFilter", chain.Type)
	}
	if *chain.Hooknum != *nftables.ChainHookPrerouting {
		t.Errorf("chain hook: got %v, want ChainHookPrerouting", *chain.Hooknum)
	}
	if *chain.Priority != *nftables.ChainPriorityRaw {
		t.Errorf("chain priority: got %v, want ChainPriorityRaw (-300)", *chain.Priority)
	}

	// rule expressions: [meta iifname, cmp iifname, meta l4proto, cmp icmp, payload type, cmp 3, payload code, cmp 4, log]
	if len(rule.Exprs) != 9 {
		t.Fatalf("rule expr count: got %d, want 9", len(rule.Exprs))
	}

	// meta iifname => reg 1
	metaIface, ok := rule.Exprs[0].(*expr.Meta)
	if !ok || metaIface.Key != expr.MetaKeyIIFNAME || metaIface.Register != 1 {
		t.Errorf("expr[0]: want Meta{Key:IIFNAME, Register:1}, got %+v", rule.Exprs[0])
	}

	// cmp eq reg 1 "eth0"
	cmpIface, ok := rule.Exprs[1].(*expr.Cmp)
	if !ok || cmpIface.Op != expr.CmpOpEq || cmpIface.Register != 1 {
		t.Errorf("expr[1]: want Cmp{Op:Eq, Register:1}, got %+v", rule.Exprs[1])
	}
	wantIFName := ifnamePad("eth0")
	for i, b := range wantIFName {
		if cmpIface.Data[i] != b {
			t.Errorf("expr[1].Data[%d]: got %x, want %x", i, cmpIface.Data[i], b)
		}
	}

	// meta l4proto => reg 1
	metaL4, ok := rule.Exprs[2].(*expr.Meta)
	if !ok || metaL4.Key != expr.MetaKeyL4PROTO || metaL4.Register != 1 {
		t.Errorf("expr[2]: want Meta{Key:L4PROTO, Register:1}, got %+v", rule.Exprs[2])
	}

	// cmp eq reg 1 IPPROTO_ICMP
	cmpL4, ok := rule.Exprs[3].(*expr.Cmp)
	if !ok || cmpL4.Op != expr.CmpOpEq || cmpL4.Data[0] != unix.IPPROTO_ICMP {
		t.Errorf("expr[3]: want Cmp{Op:Eq, Data:[1]}, got %+v", rule.Exprs[3])
	}

	// payload network header offset 9 len 1 (ip protocol field) — wait, we already
	// used l4proto for that; exprs[4] is icmp type from transport header offset 0
	payloadType, ok := rule.Exprs[4].(*expr.Payload)
	if !ok || payloadType.Base != expr.PayloadBaseTransportHeader ||
		payloadType.Offset != 0 || payloadType.Len != 1 || payloadType.DestRegister != 1 {
		t.Errorf("expr[4]: want Payload{transport,off=0,len=1,reg=1}, got %+v", rule.Exprs[4])
	}

	// cmp eq reg 1 3 (ICMP type destination-unreachable)
	cmpType, ok := rule.Exprs[5].(*expr.Cmp)
	if !ok || cmpType.Op != expr.CmpOpEq || cmpType.Data[0] != 3 {
		t.Errorf("expr[5]: want Cmp{Op:Eq, Data:[3]}, got %+v", rule.Exprs[5])
	}

	// payload transport header offset 1 len 1 (icmp code)
	payloadCode, ok := rule.Exprs[6].(*expr.Payload)
	if !ok || payloadCode.Base != expr.PayloadBaseTransportHeader ||
		payloadCode.Offset != 1 || payloadCode.Len != 1 || payloadCode.DestRegister != 1 {
		t.Errorf("expr[6]: want Payload{transport,off=1,len=1,reg=1}, got %+v", rule.Exprs[6])
	}

	// cmp eq reg 1 4 (ICMP code frag-needed)
	cmpCode, ok := rule.Exprs[7].(*expr.Cmp)
	if !ok || cmpCode.Op != expr.CmpOpEq || cmpCode.Data[0] != 4 {
		t.Errorf("expr[7]: want Cmp{Op:Eq, Data:[4]}, got %+v", rule.Exprs[7])
	}

	// log group 33
	logExpr, ok := rule.Exprs[8].(*expr.Log)
	if !ok {
		t.Fatalf("expr[8]: want *expr.Log, got %T", rule.Exprs[8])
	}
	if logExpr.Group != 33 {
		t.Errorf("log group: got %d, want 33", logExpr.Group)
	}
	wantKey := uint32(1 << unix.NFTA_LOG_GROUP)
	if logExpr.Key != wantKey {
		t.Errorf("log key: got %d, want %d", logExpr.Key, wantKey)
	}
}

func TestIfnamePadTooLong(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ifnamePad should panic for names > 15 bytes")
		}
	}()
	ifnamePad("this_is_a_very_long_interface_name")
}
