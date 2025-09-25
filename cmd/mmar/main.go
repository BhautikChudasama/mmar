package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/yusuf-musleh/mmar/constants"
	"github.com/yusuf-musleh/mmar/internal/client"
	"github.com/yusuf-musleh/mmar/internal/server"
	"github.com/yusuf-musleh/mmar/internal/utils"
)

func main() {
	serverCmd := flag.NewFlagSet(constants.SERVER_CMD, flag.ExitOnError)
	serverHttpPort := serverCmd.String(
		"http-port",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_SERVER_HTTP_PORT, constants.SERVER_HTTP_PORT),
		constants.SERVER_HTTP_PORT_HELP,
	)
	serverTcpPort := serverCmd.String(
		"tcp-port",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_SERVER_TCP_PORT, constants.SERVER_TCP_PORT),
		constants.SERVER_TCP_PORT_HELP,
	)
	serverApiKeysFile := serverCmd.String(
		"api-keys-file",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_API_KEYS_FILE, "api-keys.json"),
		constants.SERVER_API_KEYS_FILE_HELP,
	)

	clientCmd := flag.NewFlagSet(constants.CLIENT_CMD, flag.ExitOnError)
	clientLocalPort := clientCmd.String(
		"local-port",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_LOCAL_PORT, constants.CLIENT_LOCAL_PORT),
		constants.CLIENT_LOCAL_PORT_HELP,
	)
	clientTunnelHttpPort := clientCmd.String(
		"tunnel-http-port",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_TUNNEL_HTTP_PORT, constants.TUNNEL_HTTP_PORT),
		constants.CLIENT_HTTP_PORT_HELP,
	)
	clientTunnelTcpPort := clientCmd.String(
		"tunnel-tcp-port",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_TUNNEL_TCP_PORT, constants.SERVER_TCP_PORT),
		constants.CLIENT_TCP_PORT_HELP,
	)
	clientTunnelHost := clientCmd.String(
		"tunnel-host",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_TUNNEL_HOST, constants.TUNNEL_HOST),
		constants.TUNNEL_HOST_HELP,
	)
	clientCustomDns := clientCmd.String(
		"custom-dns",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_CUSTOM_DNS, ""),
		constants.CLIENT_CUSTOM_DNS_HELP,
	)
	clientCustomCert := clientCmd.String(
		"custom-cert",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_CUSTOM_CERT, ""),
		constants.CLIENT_CUSTOM_CERT_HELP,
	)
	clientCustomName := clientCmd.String(
		"custom-name",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_CUSTOM_NAME, ""),
		constants.CLIENT_CUSTOM_NAME_HELP,
	)
	clientAPIKey := clientCmd.String(
		"api-key",
		utils.EnvVarOrDefault(constants.MMAR_ENV_VAR_API_KEY, ""),
		constants.CLIENT_AUTH_TOKEN_HELP,
	)

	versionCmd := flag.NewFlagSet(constants.VERSION_CMD, flag.ExitOnError)
	versionCmd.Usage = utils.MmarVersionUsage

	flag.Usage = utils.MmarUsage

	if len(os.Args) < 2 {
		utils.MmarUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case constants.SERVER_CMD:
		serverCmd.Parse(os.Args[2:])
		mmarServerConfig := server.ConfigOptions{
			HttpPort:    *serverHttpPort,
			TcpPort:     *serverTcpPort,
			ApiKeysFile: *serverApiKeysFile,
		}
		server.Run(mmarServerConfig)
	case constants.CLIENT_CMD:
		clientCmd.Parse(os.Args[2:])
		mmarClientConfig := client.ConfigOptions{
			LocalPort:      *clientLocalPort,
			TunnelHttpPort: *clientTunnelHttpPort,
			TunnelTcpPort:  *clientTunnelTcpPort,
			TunnelHost:     *clientTunnelHost,
			CustomDns:      *clientCustomDns,
			CustomCert:     *clientCustomCert,
			CustomName:     *clientCustomName,
			APIKey:         *clientAPIKey,
		}
		client.Run(mmarClientConfig)
	case constants.VERSION_CMD:
		versionCmd.Parse(os.Args[2:])
		fmt.Println("mmar version", constants.MMAR_VERSION)
	default:
		utils.MmarUsage()
	}
}
