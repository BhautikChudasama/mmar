package constants

const (
	MMAR_VERSION = "0.2.6"

	VERSION_CMD       = "version"
	SERVER_CMD        = "server"
	CLIENT_CMD        = "client"
	CLIENT_LOCAL_PORT = "8000"
	SERVER_HTTP_PORT  = "3376"
	SERVER_TCP_PORT   = "6673"
	TUNNEL_HOST       = "mmar.dev"
	TUNNEL_HTTP_PORT  = "443"

	MMAR_ENV_VAR_SERVER_HTTP_PORT = "MMAR__SERVER_HTTP_PORT"
	MMAR_ENV_VAR_SERVER_TCP_PORT  = "MMAR__SERVER_TCP_PORT"
	MMAR_ENV_VAR_LOCAL_PORT       = "MMAR__LOCAL_PORT"
	MMAR_ENV_VAR_TUNNEL_HTTP_PORT = "MMAR__TUNNEL_HTTP_PORT"
	MMAR_ENV_VAR_TUNNEL_TCP_PORT  = "MMAR__TUNNEL_TCP_PORT"
	MMAR_ENV_VAR_TUNNEL_HOST      = "MMAR__TUNNEL_HOST"
	MMAR_ENV_VAR_CUSTOM_DNS       = "MMAR__CUSTOM_DNS"
	MMAR_ENV_VAR_CUSTOM_CERT      = "MMAR__CUSTOM_CERT"
	MMAR_ENV_VAR_CUSTOM_NAME      = "MMAR__CUSTOM_NAME"
	MMAR_ENV_VAR_API_KEY          = "MMAR__API_KEY"
	MMAR_ENV_VAR_API_KEYS_FILE    = "MMAR__API_KEYS_FILE"

	SERVER_STATS_DEFAULT_USERNAME = "admin"
	SERVER_STATS_DEFAULT_PASSWORD = "admin"

	SERVER_HTTP_PORT_HELP = "Define port where mmar will bind to and run on server for HTTP requests."
	SERVER_TCP_PORT_HELP  = "Define port where mmar will bind to and run on server for TCP connections."

	CLIENT_LOCAL_PORT_HELP    = "Define the port where your local dev server is running to expose through mmar."
	CLIENT_HTTP_PORT_HELP     = "Define port of mmar HTTP server to make requests through the tunnel."
	CLIENT_TCP_PORT_HELP      = "Define port of mmar TCP server for client to connect to, creating a tunnel."
	TUNNEL_HOST_HELP          = "Define host domain of mmar server for client to connect to."
	CLIENT_CUSTOM_DNS_HELP    = "Define a custom DNS server that the mmar client should use when accessing your local dev server. (eg: 8.8.8.8:53, defaults to DNS in OS)"
	CLIENT_CUSTOM_CERT_HELP   = "Define path to file custom TLS certificate containing complete ASN.1 DER content (certificate, signature algorithm and signature). Currently used for testing, but may be used to allow mmar client to work with a dev server using custom TLS certificate setups. (eg: /path/to/cert)"
	CLIENT_CUSTOM_NAME_HELP   = "Define a custom name for the tunnel subdomain. If not provided, a random subdomain will be generated. (eg: myapp, myproject)"
	CLIENT_AUTH_TOKEN_HELP    = "Define authentication token required to create tunnels. Must match a key in the server's API keys file."
	SERVER_API_KEYS_FILE_HELP = "Define path to YAML file containing API keys and their tunnel limits. (eg: /path/to/api-keys.yaml)"

	TUNNEL_MESSAGE_PROTOCOL_VERSION = 4
	TUNNEL_MESSAGE_DATA_DELIMITER   = '\n'
	ID_CHARSET                      = "abcdefghijklmnopqrstuvwxyz0123456789"
	ID_LENGTH                       = 6

	MAX_TUNNELS_PER_IP            = 5
	TUNNEL_RECONNECT_TIMEOUT      = 3
	GRACEFUL_SHUTDOWN_TIMEOUT     = 3
	TUNNEL_CREATE_TIMEOUT         = 3
	REQ_BODY_READ_CHUNK_TIMEOUT   = 3
	DEST_REQUEST_TIMEOUT          = 30
	HEARTBEAT_FROM_SERVER_TIMEOUT = 5
	HEARTBEAT_FROM_CLIENT_TIMEOUT = 2
	READ_DEADLINE                 = 3
	MAX_REQ_BODY_SIZE             = 10000000 // 10mb
	REQUEST_ID_BUFF_SIZE          = 4

	CLIENT_DISCONNECT_ERR_TEXT                    = "Tunnel is closed, cannot connect to mmar client."
	LOCALHOST_NOT_RUNNING_ERR_TEXT                = "Tunneled successfully, but nothing is running on localhost."
	DEST_REQUEST_TIMEDOUT_ERR_TEXT                = "Destination server took too long to respond"
	READ_BODY_CHUNK_ERR_TEXT                      = "Error reading request body"
	READ_BODY_CHUNK_TIMEOUT_ERR_TEXT              = "Timeout reading request body"
	READ_RESP_BODY_ERR_TEXT                       = "Could not read response from destination server, check your server's logs for any errors."
	MAX_REQ_BODY_SIZE_ERR_TEXT                    = "Request too large"
	FAILED_TO_FORWARD_TO_MMAR_CLIENT_ERR_TEXT     = "Failed to forward request to mmar client"
	FAILED_TO_READ_RESP_FROM_MMAR_CLIENT_ERR_TEXT = "Fail to read response from mmad client"
	INVALID_SUBDOMAIN_NAME_ERR_TEXT               = "Invalid subdomain name. Subdomain must be 1-63 characters long, contain only alphanumeric characters and hyphens, and cannot start or end with a hyphen."
	SUBDOMAIN_ALREADY_TAKEN_ERR_TEXT              = "Subdomain name is already taken. Please choose a different name."
	AUTH_TOKEN_REQUIRED_ERR_TEXT                  = "Authentication token is required to create tunnels."
	AUTH_TOKEN_INVALID_ERR_TEXT                   = "Invalid authentication token provided."
	AUTH_TOKEN_LIMIT_EXCEEDED_ERR_TEXT            = "Tunnel limit exceeded for this authentication token."

	// TERMINAL ANSI ESCAPED COLORS
	DEFAULT_COLOR = ""
	RED           = "\033[31m"
	GREEN         = "\033[32m"
	YELLOW        = "\033[33m"
	BLUE          = "\033[34m"
	RESET         = "\033[0m"
)

var (
	MMAR_SUBCOMMANDS = [][]string{
		{"server", "Runs a mmar server. Run this on your publicly reachable server if you're self-hosting mmar."},
		{"client", "Runs a mmar client. Run this on your machine to expose your localhost on a public URL."},
		{"version", "Prints the installed version of mmar."},
	}
)
