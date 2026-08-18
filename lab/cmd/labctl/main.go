// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e
// +build e2e

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"github.com/sapcc/go-pmtud/lab"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: labctl {provision,teardown,deploy}\n")
		flag.CommandLine.Usage()
		return
	}

	ctx := context.Background()

	switch args[0] {
	case "provision":
		l, err := lab.Provision(ctx)
		if err != nil {
			log.Fatalf("provision: %v", err)
		}
		fmt.Printf("Lab provisioned: %+v\n", l)

	case "teardown":
		l, _ := lab.Attach(ctx)
		if l != nil {
			if err := l.Teardown(ctx); err != nil {
				log.Fatalf("teardown: %v", err)
			}
		}
		fmt.Println("Lab torn down")

	case "deploy":
		backend := "udp"
		if len(args) > 1 {
			backend = args[1]
		}
		l, err := lab.Attach(ctx)
		if err != nil {
			log.Fatalf("attach: %v", err)
		}
		if err := l.DeployBackend(ctx, backend); err != nil {
			log.Fatalf("deploy: %v", err)
		}
		fmt.Printf("Deployed %s backend\n", backend)

	default:
		fmt.Fprintf(flag.CommandLine.Output(), "unknown command: %s\n", args[0])
		flag.CommandLine.Usage()
	}
}
