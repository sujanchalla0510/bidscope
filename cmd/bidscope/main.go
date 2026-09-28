// Command bidscope is the BidScope CLI: score SSP supply quality from
// OpenRTB bid request samples.
//
// BidScope is local-first: it reads files on your machine and never sends
// data anywhere. See https://github.com/sujanchalla0510/bidscope
package main

import (
	"os"

	"github.com/sujanchalla0510/bidscope/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
