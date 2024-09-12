package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
)

const (
	SERVER_CMD  = "server"
	CLIENT_CMD  = "client"
	CLIENT_PORT = "8000"
	SERVER_HTTP_PORT = "3376"
	SERVER_TCP_PORT = "6673"
	TUNNEL_HOST = "https://mmar.dev"
)

// use mmar like so:
// server tunnel:
// $ mmar server --http-port 8080 --tcp-port 9090

// client machine
// # mmar client --port 4664 --tunnel-host custom.domain.com

func invalidSubcommands() {
	fmt.Println("Add the subcommand 'server' or 'client'")
	os.Exit(0)
}

type Tunnel struct {
	id 		string
	conn 	net.Conn
}

func (t Tunnel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s - %s?%s", r.Method, html.EscapeString(r.URL.Path), r.URL.RawQuery)
	fmt.Fprintf(w, "Received: %s %q", r.Method, html.EscapeString(r.URL.Path))

	// Writing request to buffer to forward it
	var requestBuff bytes.Buffer
	r.Write(&requestBuff)

	if _, err := t.conn.Write(requestBuff.Bytes()); err != nil {
		log.Fatal(err)
	}

	w.Write([]byte("Got Request!"))
}

func (t Tunnel) handleTcpConnection() {
	log.Printf("TCP Conn from %s", t.conn.LocalAddr().String())
	status, err := bufio.NewReader(t.conn).ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read data from TCP conn: %v", err)
	}
	fmt.Printf("status from client: %s", status)

	// TODO: Handle non-HTTP request data being sent to mmar client gracefully

	// if _, err := t.conn.Write([]byte("Got your TCP Request!\n")); err != nil {
	// 	log.Fatal(err)
	// }
}

func runMmarServer(tcpPort string, httpPort string) {
	mux := http.NewServeMux()
	tunnel := Tunnel{id: "abc123"}

	go func() {
		log.Print("Listening for TCP Requests...")
		ln, err := net.Listen("tcp", fmt.Sprintf(":%s", tcpPort))
		if err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
			return
		}
		for {
			conn, err := ln.Accept()
			defer conn.Close()
			if err != nil {
				log.Fatalf("Failed to accept TCP connection: %v", err)
			}
			tunnel.conn = conn
			// TODO: Figure out a better placement for this, to avoid race condition
			mux.Handle("/", tunnel)
			go tunnel.handleTcpConnection()
		}
	}()

	log.Print("Listening for HTTP Requests...")
	http.ListenAndServe(fmt.Sprintf(":%s", httpPort), mux)

}

func runMmarClient(serverTcpPort string, tunnelHost string) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", tunnelHost, serverTcpPort))
	defer conn.Close()

	if err != nil {
		log.Fatalf("Failed to connect to TCP server: %v", err)
	}

	conn.Write([]byte("Hello from local client!\n"))
	for {
		// TODO: Handle non-HTTP request data being sent to mmar client gracefully, see above TODO
		// status, err := bufio.NewReader(conn).ReadBytes('\n')
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			log.Fatalf("Failed to read data from TCP conn: %v", err)
		}
		fmt.Printf("status from server: %v", req)
		fmt.Printf("body: %s", req.Body)
		// TODO: Implementing forwarding request to local dev server running
		// and forward response back to mmar server
	}
}

func main() {
	serverCmd := flag.NewFlagSet(SERVER_CMD, flag.ExitOnError)
	serverHttpPort := serverCmd.String(
		"http-port", SERVER_HTTP_PORT, "define port where mmar will bind to and run on server for HTTP requests.",
	)
	serverTcpPort := serverCmd.String(
		"tcp-port", SERVER_TCP_PORT, "define port where mmar will bind to and run on server for TCP connections.",
	)

	clientCmd := flag.NewFlagSet(CLIENT_CMD, flag.ExitOnError)
	clientPort := clientCmd.String(
		"port", CLIENT_PORT, "define a port where mmar will bind to and run will run on client.",
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
		fmt.Println("  http port:", *serverHttpPort)
		fmt.Println("  tcp port:", *serverTcpPort)
		fmt.Println("  tail:", serverCmd.Args())
		runMmarServer(*serverTcpPort, *serverHttpPort)
	case CLIENT_CMD:
		clientCmd.Parse(os.Args[2:])
		fmt.Println("subcommand 'client'")
		fmt.Println("  port:", *clientPort)
		fmt.Println("  tunnel-host:", *clientTunnelHost)
		fmt.Println("  tail:", clientCmd.Args())
		runMmarClient(*serverTcpPort, *clientTunnelHost)
	default:
		invalidSubcommands()
	}
}
