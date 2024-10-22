package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"
)

const (
	SERVER_CMD       = "server"
	CLIENT_CMD       = "client"
	CLIENT_PORT      = "8000"
	SERVER_HTTP_PORT = "3376"
	SERVER_TCP_PORT  = "6673"
	TUNNEL_HOST      = "https://mmar.dev"

	HEARTBEAT_INTERVAL        = 5 * time.Second
	GRACEFUL_SHUTDOWN_TIMEOUT = 3 * time.Second
	READ_BUFFER_SIZE          = 10
)

const (
	HEARTBEAT = iota + 1
	REQUEST
	RESPONSE
	CLIENT_DISCONNECT
	NO_LOCALHOST_RUNNING
)

var MESSAGE_MAPPING = map[int]string{
	HEARTBEAT:            "HEARTBEAT",
	REQUEST:              "REQUEST",
	RESPONSE:             "RESPONSE",
	CLIENT_DISCONNECT:    "CLIENT_DISCONNECT",
	NO_LOCALHOST_RUNNING: "NO_LOCALHOST_RUNNING",
}

// use mmar like so:
// server tunnel:
// $ mmar server --http-port 8080 --tcp-port 9090

// client machine
// # mmar client --port 4664 --tunnel-host custom.domain.com

func invalidSubcommands() {
	fmt.Println("Add the subcommand 'server' or 'client'")
	os.Exit(0)
}

type IncomingRequest struct {
	responseChannel chan OutgoingResponse
	responseWriter  http.ResponseWriter
	request         *http.Request
	cancel          context.CancelFunc
}

type OutgoingResponse struct {
	statusCode int
	body       []byte
}

type TunnelInterface interface {
	processTunnelMessages(ctx context.Context)
}

type Tunnel struct {
	id   string
	conn net.Conn
}

// Tunnel to Client
type ClientTunnel struct {
	Tunnel
	incomingChannel chan IncomingRequest
	outgoingChannel chan TunnelMessage
	heartbeatTicker *time.Ticker
}

// Tunnel to Server
type ServerTunnel struct {
	Tunnel
}

type TunnelMessage struct {
	msgType int
	msgData []byte
}

func (ct *ClientTunnel) close() {
	log.Printf("Client disconnected: %v, closing tunnel...", ct.conn.LocalAddr().String())
	// Close the TunneledRequests channel
	close(ct.incomingChannel)
	// Clear the TunneledRequests channel
	ct.incomingChannel = nil
	// Close the TunneledResponses channel
	close(ct.outgoingChannel)
	// Clear the TunneledResponses channel
	ct.outgoingChannel = nil
}

func (tm *TunnelMessage) serializeMessage() ([]byte, error) {
	serializedMsg := [][]byte{}

	// TODO: Come up with more effecient protocol for server<->client
	// Determine message type to add prefix
	msgType := MESSAGE_MAPPING[tm.msgType]
	if msgType == "" {
		log.Fatalf("Invalid TunnelMessage type: %v:", tm.msgType)
	}

	// Add the message type
	serializedMsg = append(serializedMsg, []byte(msgType))
	// Add message data bytes length
	serializedMsg = append(serializedMsg, []byte(strconv.Itoa(len(tm.msgData))))
	// Add the message data
	serializedMsg = append(serializedMsg, tm.msgData)

	// Combine all the data separated by new lines
	return bytes.Join(serializedMsg, []byte("\n")), nil
}

func (tm *TunnelMessage) readMessageData(length int, reader *bufio.Reader) []byte {
	msgData := make([]byte, length)

	if _, err := io.ReadFull(reader, msgData); err != nil {
		log.Fatalf("Failed to read all Msg Data: %v", err)
	}

	return msgData
}

func (tm *TunnelMessage) deserializeMessage(reader *bufio.Reader) error {
	msgPrefix, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	msgLengthStr, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	msgLength, err := strconv.Atoi(msgLengthStr[:len(msgLengthStr)-1])
	if err != nil {
		log.Fatalf("Could not parse message length: %v", msgLengthStr)
	}

	var msgType int
	msgData := tm.readMessageData(msgLength, reader)

	switch msgPrefix {
	case "HEARTBEAT\n":
		log.Printf("Got HEARTBEAT\n")
		msgType = HEARTBEAT
	case "REQUEST\n":
		msgType = REQUEST
	case "RESPONSE\n":
		msgType = RESPONSE
	case "CLIENT_DISCONNECT\n":
		msgType = CLIENT_DISCONNECT
	case "NO_LOCALHOST_RUNNING\n":
		msgType = NO_LOCALHOST_RUNNING
	default:
		// TODO: Gracefully handle non-protocol message received
		log.Fatalf("Invalid TunnelMessage prefix: %v", msgPrefix)
	}

	tm.msgType = msgType
	tm.msgData = msgData

	return nil
}

func (t *Tunnel) sendMessage(tunnelMsg TunnelMessage) error {
	// Serialize tunnel message data
	serializedMsg, serializeErr := tunnelMsg.serializeMessage()
	if serializeErr != nil {
		return serializeErr
	}
	_, err := t.conn.Write(serializedMsg)
	return err
}

func (t *Tunnel) receiveMessage() (TunnelMessage, error) {
	msgReader := bufio.NewReader(t.conn)

	// Read and deserialize tunnel message data
	tunnelMessage := TunnelMessage{}
	deserializeErr := tunnelMessage.deserializeMessage(msgReader)

	return tunnelMessage, deserializeErr
}

// TODO: This should probably change and should not have `ClientTunnel` as the receiver
func (t *ClientTunnel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s - %s%s", r.Method, html.EscapeString(r.URL.Path), r.URL.RawQuery)

	// Create response channel for tunneled request
	respChannel := make(chan OutgoingResponse)

	// Check if the tunnel was closed, if so,
	// send back HTTP response right away
	if t.incomingChannel == nil {
		w.Write([]byte("Tunnel is closed, cannot connect to mmar client."))
		return
	}

	ctx, cancel := context.WithCancel(r.Context())

	// Tunnel the request
	t.incomingChannel <- IncomingRequest{
		responseChannel: respChannel,
		responseWriter:  w,
		request:         r,
		cancel:          cancel,
	}

	select {
	case <-ctx.Done(): // Tunnel is closed if context is cancelled
		w.Write([]byte("Tunnel is closed, cannot connect to mmar client."))
		return
	case resp, _ := <-respChannel: // Await response for tunneled request
		// Write response headers with response status code to original client
		w.WriteHeader(resp.statusCode)

		// Write the response body to original client
		w.Write(resp.body)
	}

}

func (ct *ClientTunnel) processTunneledRequests() {
	for {
		// Read requests coming in tunnel channel
		incomingReq, ok := <-ct.incomingChannel
		if !ok {
			// Channel closed, client disconencted, shutdown goroutine
			return
		}

		// Writing request to buffer to forward it
		var requestBuff bytes.Buffer
		incomingReq.request.Write(&requestBuff)

		// Forward the request to mmar client
		reqMessage := TunnelMessage{msgType: REQUEST, msgData: requestBuff.Bytes()}
		if err := ct.sendMessage(reqMessage); err != nil {
			log.Fatal(err)
		}

		// Wait for response for this request to come back from outgoing channel
		respTunnelMsg, _ := <-ct.outgoingChannel

		// Read response for forwarded request
		respReader := bufio.NewReader(bytes.NewReader(respTunnelMsg.msgData))
		resp, respErr := http.ReadResponse(respReader, incomingReq.request)

		if respErr != nil {
			if errors.Is(respErr, io.ErrUnexpectedEOF) || errors.Is(respErr, net.ErrClosed) {
				incomingReq.cancel()
				ct.close()
				return
			}
			failedReq := fmt.Sprintf("%s - %s%s", incomingReq.request.Method, html.EscapeString(incomingReq.request.URL.Path), incomingReq.request.URL.RawQuery)
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
			incomingReq.responseWriter.Header().Set(hKey, hVal[0])
			// Add remaining values for header if more than than one exists
			for i := 1; i < len(hVal); i++ {
				incomingReq.responseWriter.Header().Add(hKey, hVal[i])
			}
		}

		// Send response back to goroutine handling the request
		incomingReq.responseChannel <- OutgoingResponse{statusCode: resp.StatusCode, body: respBody}
	}
}

func (ct *ClientTunnel) processTunnelMessages() {
	for {
		tunnelMsg, err := ct.receiveMessage()
		if err != nil {
			log.Fatalf("Failed to receive message from server tunnel: %v", err)
		}

		switch tunnelMsg.msgType {
		case HEARTBEAT:
			// TODO: Need to implement some heartbeat logic here
			log.Printf("Got HEARTBEAT TUNNEL MESSAGE\n")
			continue
		case RESPONSE:
			log.Printf("Got RESPONSE TUNNEL MESSAGE\n")
			ct.outgoingChannel <- tunnelMsg
		case CLIENT_DISCONNECT:
			log.Printf("Got CLIENT_DISCONNECT TUNNEL MESSAGE\n")
			ct.close()
			return
		}
	}
}

// TODO: Move this logic to the client
func (ct *ClientTunnel) startHeartbeat() {
	ct.heartbeatTicker = time.NewTicker(HEARTBEAT_INTERVAL)

	for {
		<-ct.heartbeatTicker.C
		// Send heartbeat, if it fails that means the client diconnected
		heartbeatMessage := TunnelMessage{msgType: HEARTBEAT}
		if err := ct.sendMessage(heartbeatMessage); err != nil {
			ct.close()
			return
		}
	}
}

func (ct *ClientTunnel) handleTcpConnection() {
	log.Printf("TCP Conn from %s", ct.conn.LocalAddr().String())

	// Create channel to tunnel request to
	ct.incomingChannel = make(chan IncomingRequest)
	ct.outgoingChannel = make(chan TunnelMessage)

	// Process Tunnel Messages coming from mmar client
	go ct.processTunnelMessages()

	// Start goroutine to process tunneled requests
	go ct.processTunneledRequests()
}

func runMmarServer(tcpPort string, httpPort string) {
	// Channel handler for interrupt signal
	sigInt := make(chan os.Signal, 1)
	signal.Notify(sigInt, os.Interrupt)

	mux := http.NewServeMux()
	clientTunnel := ClientTunnel{}
	mux.Handle("/", &clientTunnel)

	go func() {
		log.Print("Listening for TCP Requests...")
		ln, err := net.Listen("tcp", fmt.Sprintf(":%s", tcpPort))
		if err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
			return
		}
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Fatalf("Failed to accept TCP connection: %v", err)
			}
			clientTunnel.conn = conn
			go clientTunnel.handleTcpConnection()
		}
	}()

	go func() {
		log.Print("Listening for HTTP Requests...")
		if err := http.ListenAndServe(fmt.Sprintf(":%s", httpPort), mux); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Error listening and serving: %s\n", err)
		}
	}()

	// Wait for an interrupt signal, if received, terminate gracefully
	<-sigInt
	log.Printf("Gracefully shutting down server...")
}

func localizeRequest(request *http.Request) {
	localhost := fmt.Sprintf("http://localhost:%v%v", CLIENT_PORT, request.RequestURI)
	localURL, urlErr := url.Parse(localhost)
	if urlErr != nil {
		log.Fatalf("Failed to parse URL: %v", urlErr)
	}

	// Set URL to send request to local server
	request.URL = localURL
	// Clear requestURI since it is now a client request
	request.RequestURI = ""
}

// Process requests coming from mmar server and forward them to localhost
func (st *ServerTunnel) handleRequestMessage(tunnelMsg TunnelMessage) {
	fwdClient := &http.Client{}

	reqReader := bufio.NewReader(bytes.NewReader(tunnelMsg.msgData))
	req, reqErr := http.ReadRequest(reqReader)

	// TODO: Might need to remove this??
	if reqErr != nil {
		if errors.Is(reqErr, io.EOF) {
			log.Print("Connection to mmar server closed or disconnected. Exiting...")
			os.Exit(0)
		}

		if errors.Is(reqErr, net.ErrClosed) {
			log.Printf("Connection closed.")
			os.Exit(0)
		}
		log.Fatalf("Failed to read data from TCP conn: %v", reqErr)
	}

	// Convert request to target localhost
	localizeRequest(req)

	log.Printf("%s - %s%s", req.Method, html.EscapeString(req.URL.Path), req.URL.RawQuery)
	resp, fwdErr := fwdClient.Do(req)
	if fwdErr != nil {
		localhostNotRunningMsg := []byte("Tunnel is open but nothing is running on localhost")
		if _, err := st.conn.Write(localhostNotRunningMsg); err != nil {
			log.Fatalf("Failed to forward: %v", fwdErr)
		}
	}

	// Writing response to buffer to tunnel it back
	var responseBuff bytes.Buffer
	resp.Write(&responseBuff)

	respMessage := TunnelMessage{msgType: RESPONSE, msgData: responseBuff.Bytes()}
	if err := st.sendMessage(respMessage); err != nil {
		log.Fatal(err)
	}
}

func (st *ServerTunnel) processTunnelMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done(): // Client gracefully shutdown
			return
		default:
			tunnelMsg, err := st.receiveMessage()
			if err != nil {
				if errors.Is(err, io.EOF) {
					log.Print("Tunnel connection closed from Server. Exiting...")
					os.Exit(0)
				} else if errors.Is(err, net.ErrClosed) {
					log.Print("Tunnel connection disconnected from Server. Existing...")
					os.Exit(0)
				}
				log.Fatalf("Failed to receive message from server tunnel: %v", err)
			}

			switch tunnelMsg.msgType {
			case HEARTBEAT:
				// TODO: Might want to switch this around to have client send
				// heartbeats instead of server
				log.Printf("Got HEARTBEAT TUNNEL MESSAGE\n")
				continue
			case REQUEST:
				log.Printf("Got REQUEST TUNNEL MESSAGE\n")
				go st.handleRequestMessage(tunnelMsg)
			}
		}
	}
}

func runMmarClient(serverTcpPort string, tunnelHost string) {
	// Channel handler for interrupt signal
	sigInt := make(chan os.Signal, 1)
	signal.Notify(sigInt, os.Interrupt)

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", tunnelHost, serverTcpPort))
	if err != nil {
		log.Printf("Could not reach mmar server on: %s:%s \nExiting...", tunnelHost, serverTcpPort)
		os.Exit(0)
	}
	defer conn.Close()
	serverTunnel := ServerTunnel{Tunnel{conn: conn}}

	// Create context to cancel running gouroutines when shutting down
	ctx, cancel := context.WithCancel(context.Background())

	// Process Tunnel Messages coming from mmar server
	go serverTunnel.processTunnelMessages(ctx)

	// Wait for an interrupt signal, if received, terminate gracefully
	<-sigInt

	log.Printf("Gracefully shutting down client...")
	disconnectMsg := TunnelMessage{msgType: CLIENT_DISCONNECT}
	serverTunnel.sendMessage(disconnectMsg)
	cancel()
	gracefulShutdownTimer := time.NewTimer(GRACEFUL_SHUTDOWN_TIMEOUT)
	<-gracefulShutdownTimer.C
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
