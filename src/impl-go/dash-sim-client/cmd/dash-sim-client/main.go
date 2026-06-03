// Command dash-sim-client is the operator CLI for the dashapi.v1.DashApi gRPC
// service. It dials a running DashApi server (the dash-sim simulator, or — in
// phase 3 — a vendor adapter on real DASH-compliant DPU hardware) and
// exposes every RPC as a subcommand.
//
// Quick start:
//
//	dash-sim-client kinds
//	dash-sim-client apply --kind vnet --key vnet-prod --value '{"vni":1001}'
//	dash-sim-client list  --kind vnet -o table
//	dash-sim-client subscribe --snapshot --kinds vnet,eni
package main

import "github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/cmd"

func main() { cmd.Execute() }
