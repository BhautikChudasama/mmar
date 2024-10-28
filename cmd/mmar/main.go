package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/yusuf-musleh/mmar/constants"
	"github.com/yusuf-musleh/mmar/internal/client"
	"github.com/yusuf-musleh/mmar/internal/server"
)

func invalidSubcommands() {
	fmt.Println("Add the subcommand 'server' or 'client'")
	os.Exit(0)
}

func main() {
	serverCmd := flag.NewFlagSet(constants.SERVER_CMD, flag.ExitOnError)
	serverHttpPort := serverCmd.String(
		"http-port", constants.SERVER_HTTP_PORT, "define port where mmar will bind to and run on server for HTTP requests.",
	)
	serverTcpPort := serverCmd.String(
		"tcp-port", constants.SERVER_TCP_PORT, "define port where mmar will bind to and run on server for TCP connections.",
	)

	clientCmd := flag.NewFlagSet(constants.CLIENT_CMD, flag.ExitOnError)
	clientPort := clientCmd.String(
		"port", constants.CLIENT_PORT, "define a port where mmar will bind to and run will run on client.",
	)
	clientTunnelHost := clientCmd.String(
		"tunnel-host", constants.TUNNEL_HOST, "define host domain of mmar server for client to connect to.",
	)

	if len(os.Args) < 2 {
		invalidSubcommands()
	}

	switch os.Args[1] {
	case constants.SERVER_CMD:
		serverCmd.Parse(os.Args[2:])
		fmt.Println("subcommand 'server'")
		fmt.Println("  http port:", *serverHttpPort)
		fmt.Println("  tcp port:", *serverTcpPort)
		fmt.Println("  tail:", serverCmd.Args())
		server.Run(*serverTcpPort, *serverHttpPort)
	case constants.CLIENT_CMD:
		clientCmd.Parse(os.Args[2:])
		fmt.Println("subcommand 'client'")
		fmt.Println("  port:", *clientPort)
		fmt.Println("  tunnel-host:", *clientTunnelHost)
		fmt.Println("  tail:", clientCmd.Args())
		client.Run(*serverTcpPort, *clientTunnelHost)
	default:
		invalidSubcommands()
	}
}
