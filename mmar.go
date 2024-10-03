package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
)

const (
	SERVER_CMD       = "server"
	CLIENT_CMD       = "client"
	CLIENT_PORT      = "8000"
	SERVER_HTTP_PORT = "3376"
	SERVER_TCP_PORT  = "6673"
	TUNNEL_HOST      = "https://mmar.dev"
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

type TunneledRequest struct {
	responseChannel chan TunneledResponse
	responseWriter  http.ResponseWriter
	request         *http.Request
}

type TunneledResponse struct {
	statusCode int
	body       []byte
}

type Tunnel struct {
	id      string
	conn    net.Conn
	channel chan TunneledRequest
}

func (t *Tunnel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s - %s%s", r.Method, html.EscapeString(r.URL.Path), r.URL.RawQuery)

	// Create response channel for tunneled request
	respChannel := make(chan TunneledResponse)

	// Tunnel the request
	t.channel <- TunneledRequest{
		responseChannel: respChannel,
		responseWriter:  w,
		request:         r,
	}

	// Await response for tunneled request
	resp, _ := <-respChannel

	// Write response headers with response status code to original client
	w.WriteHeader(resp.statusCode)

	// Write the response body to original client
	w.Write(resp.body)
}

func (t *Tunnel) processTunneledRequests() {
	for {
		// TODO: handle ok? case and gracefully exiting goroutine
		// Read requests coming in tunnel channel
		msg, _ := <-t.channel

		// Writing request to buffer to forward it
		var requestBuff bytes.Buffer
		msg.request.Write(&requestBuff)

		// Forward the request to mmar client
		if _, err := t.conn.Write(requestBuff.Bytes()); err != nil {
			log.Fatal(err)
		}

		// Read response for forwarded request
		respReader := bufio.NewReader(t.conn)
		resp, respErr := http.ReadResponse(respReader, msg.request)
		if respErr != nil {
			failedReq := fmt.Sprintf("%s - %s%s", msg.request.Method, html.EscapeString(msg.request.URL.Path), msg.request.URL.RawQuery)
			log.Fatalf("Failed to return response: %v\n\n for req: %v", respErr, failedReq)
		}
		defer resp.Body.Close()

		respBody, respBodyErr := io.ReadAll(resp.Body)
		if respBodyErr != nil {
			log.Fatalf("Failed to parse response body: %v\n\n", respBodyErr)
			os.Exit(1)
		}

		// Set headers for response
		for hKey, hVal := range resp.Header {
			msg.responseWriter.Header().Set(hKey, hVal[0])
			// Add remaining values for header if more than than one exists
			for i := 1; i < len(hVal); i++ {
				msg.responseWriter.Header().Add(hKey, hVal[i])
			}
		}

		// Send response back to goroutine handling the request
		msg.responseChannel <- TunneledResponse{statusCode: resp.StatusCode, body: respBody}
	}
}

func (t *Tunnel) handleTcpConnection() {
	log.Printf("TCP Conn from %s", t.conn.LocalAddr().String())
	_, err := bufio.NewReader(t.conn).ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read data from TCP conn: %v", err)
	}

	// Create channel to tunnel request to
	t.channel = make(chan TunneledRequest)

	// Start goroutine to process tunneled requests
	go t.processTunneledRequests()
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
			mux.Handle("/", &tunnel)
			go tunnel.handleTcpConnection()
		}
	}()

	log.Print("Listening for HTTP Requests...")
	http.ListenAndServe(fmt.Sprintf(":%s", httpPort), mux)

}

func runMmarClient(serverTcpPort string, tunnelHost string) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", tunnelHost, serverTcpPort))
	defer conn.Close()
	fwdClient := &http.Client{}

	if err != nil {
		log.Fatalf("Failed to connect to TCP server: %v", err)
	}

	conn.Write([]byte("Hello from local client!\n"))

	for {
		// TODO: Handle non-HTTP request data being sent to mmar client gracefully
		// status, err := bufio.NewReader(conn).ReadBytes('\n')
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			log.Fatalf("Failed to read data from TCP conn: %v", err)
		}

		// TODO: Extract this into a separate function
		localURL, urlErr := url.Parse(fmt.Sprintf("http://localhost:%v%v", CLIENT_PORT, req.RequestURI))
		if urlErr != nil {
			log.Fatalf("Failed to parse URL: %v", urlErr)
		}

		// Set URL to send request to local server
		req.URL = localURL
		// Clear requestURI since it is now a client request
		req.RequestURI = ""

		log.Printf("%s - %s%s", req.Method, html.EscapeString(req.URL.Path), req.URL.RawQuery)
		resp, fwdErr := fwdClient.Do(req)
		if fwdErr != nil {
			log.Fatalf("Failed to forward: %v", fwdErr)
		}

		// Writing response to buffer to tunnel it back
		var responseBuff bytes.Buffer
		resp.Write(&responseBuff)

		if _, err := conn.Write(responseBuff.Bytes()); err != nil {
			log.Fatal(err)
		}
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
