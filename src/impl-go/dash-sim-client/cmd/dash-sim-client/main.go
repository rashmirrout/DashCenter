// Command dash-sim-client is the operator CLI for the dashsim.v1.DashSim
// gRPC service. It dials a running dash-sim instance and exposes every RPC
// as a subcommand.
//
// Quick start:
//
//	dash-sim-client ping
//	dash-sim-client vnet create vnet-prod --vni 1001
//	dash-sim-client vnet list
//	dash-sim-client subscribe --snapshot --kinds vnet,eni
package main

import "github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/cmd"

func main() { cmd.Execute() }
