package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	SERVER_CMD  = "server"
	CLIENT_CMD  = "client"
	CLIENT_PORT = "8000"
	SERVER_PORT = "3376"
	TUNNEL_HOST = "https://mmar.dev"
)

// use mmar like so:
// server tunnel:
// $ mmar server --port 8080

// client machine
// # mmar client --port 4664 --tunnel-host custom.domain.com

func invalidSubcommands() {
	fmt.Println("Add the subcommand 'server' or 'client'")
	os.Exit(0)
}

func main() {

	serverCmd := flag.NewFlagSet(SERVER_CMD, flag.ExitOnError)
	serverPort := serverCmd.String(
		"port", SERVER_PORT, "define port where mmar will bind to and run on server.",
	)

	clientCmd := flag.NewFlagSet(CLIENT_CMD, flag.ExitOnError)
	clientPort := clientCmd.String(
		"port", CLIENT_PORT, "define a port where mmar will connect to client locally.",
	)
	clientTunnelHost := clientCmd.String(
		"tunnel-host", TUNNEL_HOST, "define host domain of mmar server for client to connect to.",
	)

	if len(os.Args) < 2 {
		invalidSubcommands()
	}

	switch os.Args[1] {
	case SERVER_CMD:
		serverCmd.Parse(os.Args[2:])
		fmt.Println("subcommand 'server'")
		fmt.Println("  port:", *serverPort)
		fmt.Println("  tail:", serverCmd.Args())
	case CLIENT_CMD:
		clientCmd.Parse(os.Args[2:])
		fmt.Println("subcommand 'client'")
		fmt.Println("  port:", *clientPort)
		fmt.Println("  tunnel-host:", *clientTunnelHost)
		fmt.Println("  tail:", clientCmd.Args())
	default:
		invalidSubcommands()
	}

	fmt.Println("Hello World!")
}
