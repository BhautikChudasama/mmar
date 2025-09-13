package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"time"

	"github.com/yusuf-musleh/mmar/constants"
	"github.com/yusuf-musleh/mmar/internal/logger"
	"github.com/yusuf-musleh/mmar/internal/protocol"
	"github.com/yusuf-musleh/mmar/internal/utils"
)

var CLIENT_MAX_TUNNELS_REACHED = errors.New("Client reached max tunnels limit")

type ConfigOptions struct {
	HttpPort string
	TcpPort  string
}

type MmarServer struct {
	mu           sync.Mutex
	clients      map[string]ClientTunnel
	tunnelsPerIP map[string][]string
}

type IncomingRequest struct {
	responseChannel chan OutgoingResponse
	responseWriter  http.ResponseWriter
	request         *http.Request
	cancel          context.CancelCauseFunc
	serializedReq   []byte
	ctx             context.Context
}

type OutgoingResponse struct {
	statusCode int
	body       []byte
}

// Tunnel to Client
type ClientTunnel struct {
	protocol.Tunnel
	incomingChannel chan IncomingRequest
	outgoingChannel chan protocol.TunnelMessage
}

func (ct *ClientTunnel) drainChannels() {
	// Drain all incoming requests to tunnel and cancel them
incomingDrainerLoop:
	for {
		select {
		case incoming, _ := <-ct.incomingChannel:
			// Cancel incoming requests
			incoming.cancel(CLIENT_DISCONNECTED_ERR)
		default:
			// Close the TunneledRequests channel
			close(ct.incomingChannel)
			break incomingDrainerLoop
		}
	}

	// Draining all outgoing requests from tunnel
OutgoingDrainerLoop:
	for {
		select {
		case <-ct.outgoingChannel:
			// Just draining, do nothing
		default:
			// Close the TunneledResponses channel
			close(ct.outgoingChannel)
			break OutgoingDrainerLoop
		}
	}
}

func (ct *ClientTunnel) close(graceful bool) {
	logger.Log(
		constants.DEFAULT_COLOR,
		fmt.Sprintf(
			"[%s] Client disconnected: %v, closing tunnel...",
			ct.Tunnel.Id,
			ct.Conn.RemoteAddr().String(),
		),
	)

	// Drain channels before closing them to prevent panics if there are blocked writes
	ct.drainChannels()

	if graceful {
		// Wait a little for final response to complete, then close the connection
		gracefulCloseTimer := time.NewTimer(constants.GRACEFUL_SHUTDOWN_TIMEOUT * time.Second)
		<-gracefulCloseTimer.C
	}

	ct.Conn.Close()
	logger.Log(
		constants.DEFAULT_COLOR,
		fmt.Sprintf(
			"[%s] Tunnel connection closed: %v",
			ct.Tunnel.Id,
			ct.Conn.RemoteAddr().String(),
		),
	)
}

// Serves simple stats for mmar server behind Basic Authentication
func (ms *MmarServer) handleServerStats(w http.ResponseWriter, r *http.Request) {
	// Check Basic Authentication
	username, password, ok := r.BasicAuth()
	if !ok || !utils.ValidCredentials(username, password) {
		w.Header().Add("WWW-Authenticate", "Basic realm=\"stats\"")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	stats := map[string]any{}

	// Add total connected clients count
	stats["connectedClientsCount"] = len(ms.clients)

	// Add list of connected clients, including only relevant fields
	clientStats := []map[string]string{}
	for _, val := range ms.clients {
		client := map[string]string{
			"id":        val.Id,
			"createdOn": val.CreatedOn.Format(time.RFC3339),
		}
		clientStats = append(clientStats, client)
	}
	stats["connectedClients"] = clientStats

	// Marshal the result
	marshalledStats, err := json.Marshal(stats)

	if err != nil {
		logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to marshal server stats: %v", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(marshalledStats)
}

func (ms *MmarServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract subdomain to retrieve related client tunnel
	subdomain := utils.ExtractSubdomain(r.Host)

	// Handle stats subdomain
	if subdomain == "stats" {
		ms.handleServerStats(w, r)
		return
	}

	clientTunnel, clientExists := ms.clients[subdomain]

	if !clientExists {
		protocol.RespondTunnelErr(protocol.CLIENT_DISCONNECT, w)
		return
	}

	// Create channel to receive serialized request
	serializedReqChannel := make(chan []byte)

	ctx, cancel := context.WithCancelCause(r.Context())

	// Writing request to buffer to forward it
	go serializeRequest(ctx, r, cancel, serializedReqChannel)

	select {
	case <-ctx.Done():
		// We could not serialize request, so we cancelled it
		handleCancel(context.Cause(ctx), w)
		return
	case serializedRequest, _ := <-serializedReqChannel:
		// Request serialized, we can proceed to tunnel it

		// Create response channel to receive response for tunneled request
		respChannel := make(chan OutgoingResponse)

		// Tunnel the request
		clientTunnel.incomingChannel <- IncomingRequest{
			responseChannel: respChannel,
			responseWriter:  w,
			request:         r,
			cancel:          cancel,
			serializedReq:   serializedRequest,
			ctx:             ctx,
		}

		select {
		case <-ctx.Done(): // Request is canceled or Tunnel is closed if context is canceled
			handleCancel(context.Cause(ctx), w)
			return
		case resp, _ := <-respChannel: // Await response for tunneled request
			// Add header to close the connection
			w.Header().Set("Connection", "close")

			// Write response headers with response status code to original client
			w.WriteHeader(resp.statusCode)

			// Write the response body to original client
			w.Write(resp.body)
		}
	}
}

func (ms *MmarServer) GenerateUniqueId() string {
	reservedIDs := []string{"", "admin", "stats"}

	generatedId := ""
	for _, exists := ms.clients[generatedId]; exists || slices.Contains(reservedIDs, generatedId); {
		var randSeed *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
		b := make([]byte, constants.ID_LENGTH)
		for i := range b {
			b[i] = constants.ID_CHARSET[randSeed.Intn(len(constants.ID_CHARSET))]
		}
		generatedId = string(b)
	}

	return generatedId
}

func (ms *MmarServer) TunnelLimitedIP(ip string) bool {
	tunnels, tunnelsExist := ms.tunnelsPerIP[ip]

	// Initialize tunnels list for IP
	if !tunnelsExist {
		ms.tunnelsPerIP[ip] = []string{}
	}

	return len(tunnels) >= constants.MAX_TUNNELS_PER_IP
}

func (ms *MmarServer) newClientTunnel(tunnel protocol.Tunnel, subdomain string) (*ClientTunnel, error) {
	// Acquire lock to create new client tunnel data
	ms.mu.Lock()

	var uniqueId string
	var msgType uint8
	if subdomain != "" {
		uniqueId = subdomain
		msgType = protocol.TUNNEL_RECLAIMED
	} else {
		// Generate unique ID for client if not passed in
		uniqueId = ms.GenerateUniqueId()
		msgType = protocol.TUNNEL_CREATED
	}

	tunnel.Id = uniqueId

	// Create channels to tunnel requests to and recieve responses from
	incomingChannel := make(chan IncomingRequest)
	outgoingChannel := make(chan protocol.TunnelMessage)

	// Create client tunnel
	clientTunnel := ClientTunnel{
		tunnel,
		incomingChannel,
		outgoingChannel,
	}

	// Check if IP reached max tunnel limit
	clientIP := utils.ExtractIP(tunnel.Conn.RemoteAddr().String())
	limitedIP := ms.TunnelLimitedIP(clientIP)
	// If so, send limit message to client and close client tunnel
	if limitedIP {
		limitMessage := protocol.TunnelMessage{MsgType: protocol.CLIENT_TUNNEL_LIMIT}
		if err := clientTunnel.SendMessage(limitMessage); err != nil {
			logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to send Tunnel Limit msg to client: %v", err))
		}
		clientTunnel.close(false)
		// Release lock once errored
		ms.mu.Unlock()
		return nil, CLIENT_MAX_TUNNELS_REACHED
	}

	// Add client tunnel to clients
	ms.clients[uniqueId] = clientTunnel

	// Associate tunnel with client IP
	ms.tunnelsPerIP[clientIP] = append(ms.tunnelsPerIP[clientIP], uniqueId)

	// Release lock once created
	ms.mu.Unlock()

	// Send unique ID to client
	connMessage := protocol.TunnelMessage{MsgType: msgType, MsgData: []byte(uniqueId)}
	if err := clientTunnel.SendMessage(connMessage); err != nil {
		logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to send unique ID msg to client: %v", err))
		return nil, err
	}

	// Start goroutine to process tunneled requests
	go ms.processTunneledRequestsForClient(&clientTunnel)

	return &clientTunnel, nil
}

func (ms *MmarServer) handleTcpConnection(conn net.Conn) {

	tunnel := protocol.Tunnel{
		Conn:      conn,
		CreatedOn: time.Now(),
	}

	// Process Tunnel Messages coming from mmar client
	go ms.processTunnelMessages(tunnel)
}

func (ms *MmarServer) closeTunnel(t *protocol.Tunnel) {
	t.Conn.Close()
}

func (ms *MmarServer) closeClientTunnel(ct *ClientTunnel) {
	// Remove Client Tunnel from clients
	delete(ms.clients, ct.Id)

	// Remove Client Tunnel from client IP
	clientIP := utils.ExtractIP(ct.Conn.RemoteAddr().String())
	tunnels := ms.tunnelsPerIP[clientIP]
	index := slices.Index(tunnels, ct.Id)
	if index != -1 {
		tunnels = slices.Delete(tunnels, index, index+1)
		ms.tunnelsPerIP[clientIP] = tunnels
	}

	// Gracefully close the Client Tunnel
	ct.close(true)
}

func (ms *MmarServer) closeClientTunnelOrConn(ct *ClientTunnel, t protocol.Tunnel) {

	// If client has not reserved subdomain, just close the tcp connection
	if !ct.ReservedSubdomain() {
		ms.closeTunnel(&t)
		return
	}

	ms.closeClientTunnel(ct)
}

func (ms *MmarServer) processTunneledRequestsForClient(ct *ClientTunnel) {
	for {
		// Read requests coming in tunnel channel
		incomingReq, ok := <-ct.incomingChannel
		if !ok {
			// Channel closed, client disconencted, shutdown goroutine
			return
		}

		// Forward the request to mmar client
		reqMessage := protocol.TunnelMessage{MsgType: protocol.REQUEST, MsgData: incomingReq.serializedReq}
		if err := ct.SendMessage(reqMessage); err != nil {
			logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to send Request msg to client: %v", err))
			incomingReq.cancel(FAILED_TO_FORWARD_TO_MMAR_CLIENT_ERR)
			continue
		}

		// Wait for response for this request to come back from outgoing channel
		respTunnelMsg, ok := <-ct.outgoingChannel
		if !ok {
			// Channel closed, client disconencted, shutdown goroutine
			return
		}

		// Read response for forwarded request
		respReader := bufio.NewReader(bytes.NewReader(respTunnelMsg.MsgData))
		resp, respErr := http.ReadResponse(respReader, incomingReq.request)

		if respErr != nil {
			if errors.Is(respErr, io.ErrUnexpectedEOF) || errors.Is(respErr, net.ErrClosed) {
				incomingReq.cancel(CLIENT_DISCONNECTED_ERR)
				ms.closeClientTunnel(ct)
				return
			}
			failedReq := fmt.Sprintf("%s - %s%s", incomingReq.request.Method, html.EscapeString(incomingReq.request.URL.Path), incomingReq.request.URL.RawQuery)
			logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to return response: %v\n\n for req: %v", respErr, failedReq))
			incomingReq.cancel(FAILED_TO_READ_RESP_FROM_MMAR_CLIENT_ERR)
			continue
		}

		respBody, respBodyErr := io.ReadAll(resp.Body)
		if respBodyErr != nil {
			logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to parse response body: %v\n\n", respBodyErr))
			incomingReq.cancel(READ_RESP_BODY_ERR)
			continue
		}

		// Set headers for response
		for hKey, hVal := range resp.Header {
			incomingReq.responseWriter.Header().Set(hKey, hVal[0])
			// Add remaining values for header if more than than one exists
			for i := 1; i < len(hVal); i++ {
				incomingReq.responseWriter.Header().Add(hKey, hVal[i])
			}
		}

		// Close response body
		resp.Body.Close()

		select {
		case <-incomingReq.ctx.Done():
			// Request is canceled, on to the next request
			continue
		case incomingReq.responseChannel <- OutgoingResponse{statusCode: resp.StatusCode, body: respBody}:
			// Send response data back
		}
	}
}

func (ms *MmarServer) processTunnelMessages(t protocol.Tunnel) {
	var ct *ClientTunnel
	for {
		// Send heartbeat if nothing has been read for a while
		receiveMessageTimeout := time.AfterFunc(
			constants.HEARTBEAT_FROM_SERVER_TIMEOUT*time.Second,
			func() {
				heartbeatMsg := protocol.TunnelMessage{MsgType: protocol.HEARTBEAT_FROM_SERVER}
				if err := t.SendMessage(heartbeatMsg); err != nil {
					logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to send heartbeat: %v", err))
					ms.closeClientTunnelOrConn(ct, t)
					return
				}
				// Set a read timeout, if no response to heartbeat is recieved within that period,
				// that means the client has disconnected
				readDeadline := time.Now().Add((constants.READ_DEADLINE * time.Second))
				t.Conn.SetReadDeadline(readDeadline)
			},
		)

		tunnelMsg, err := t.ReceiveMessage()
		// If a message is received, stop the receiveMessageTimeout and remove the ReadTimeout
		// as we do not need to send heartbeat or check connection health in this iteration
		receiveMessageTimeout.Stop()
		t.Conn.SetReadDeadline(time.Time{})

		if err != nil {
			logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Receive Message from client tunnel errored: %v", err))
			if utils.NetworkError(err) {
				// If error with connection, stop processing messages
				ms.closeClientTunnelOrConn(ct, t)
				return
			}
			continue
		}

		switch tunnelMsg.MsgType {
		case protocol.CREATE_TUNNEL:
			// mmar client requesting new tunnel
			ct, err = ms.newClientTunnel(t, "")

			if err != nil {
				if errors.Is(err, CLIENT_MAX_TUNNELS_REACHED) {
					// Close the connection when client max tunnels limit reached
					t.Conn.Close()
					return
				}
				logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to create ClientTunnel: %v", err))
				return
			}

			logger.Log(
				constants.DEFAULT_COLOR,
				fmt.Sprintf(
					"[%s] Tunnel created: %s",
					ct.Tunnel.Id,
					t.Conn.RemoteAddr().String(),
				),
			)
		case protocol.RECLAIM_TUNNEL:
			// mmar client reclaiming a previously created tunnel
			existingId := string(tunnelMsg.MsgData)

			// Check if the subdomain has already been taken
			_, ok := ms.clients[existingId]
			if ok {
				// if so, close the tunnel, so the user can create a new one
				ms.closeClientTunnelOrConn(ct, t)
				return
			}

			ct, err = ms.newClientTunnel(t, existingId)
			if err != nil {
				if errors.Is(err, CLIENT_MAX_TUNNELS_REACHED) {
					// Close the connection when client max tunnels limit reached
					t.Conn.Close()
					return
				}
				logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to reclaim ClientTunnel: %v", err))
				return
			}

			logger.Log(
				constants.DEFAULT_COLOR,
				fmt.Sprintf(
					"[%s] Tunnel reclaimed: %s",
					existingId,
					ct.Conn.RemoteAddr().String(),
				),
			)
		case protocol.RESPONSE:
			ct.outgoingChannel <- tunnelMsg
		case protocol.LOCALHOST_NOT_RUNNING:
			// Create a response for Tunnel connected but localhost not running
			errState := protocol.TunnelErrState(protocol.LOCALHOST_NOT_RUNNING)
			responseBuff := createSerializedServerResp("200 OK", http.StatusOK, errState)
			notRunningMsg := protocol.TunnelMessage{MsgType: protocol.RESPONSE, MsgData: responseBuff.Bytes()}
			ct.outgoingChannel <- notRunningMsg
		case protocol.DEST_REQUEST_TIMEDOUT:
			// Create a response for Tunnel connected but localhost took too long to respond
			errState := protocol.TunnelErrState(protocol.DEST_REQUEST_TIMEDOUT)
			responseBuff := createSerializedServerResp("200 OK", http.StatusOK, errState)
			destTimedoutMsg := protocol.TunnelMessage{MsgType: protocol.RESPONSE, MsgData: responseBuff.Bytes()}
			ct.outgoingChannel <- destTimedoutMsg
		case protocol.CLIENT_DISCONNECT:
			ms.closeClientTunnelOrConn(ct, t)
			return
		case protocol.HEARTBEAT_FROM_CLIENT:
			heartbeatAckMsg := protocol.TunnelMessage{MsgType: protocol.HEARTBEAT_ACK}
			if err := t.SendMessage(heartbeatAckMsg); err != nil {
				logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to heartbeat ack to client: %v", err))
				ms.closeClientTunnelOrConn(ct, t)
				return
			}
		case protocol.HEARTBEAT_ACK:
			// Got a heartbeat ack, that means the connection is healthy,
			// we do not need to perform any action
		case protocol.INVALID_RESP_FROM_DEST:
			// Create a response for receiving invalid response from destination server
			errState := protocol.TunnelErrState(protocol.INVALID_RESP_FROM_DEST)
			responseBuff := createSerializedServerResp("500 Internal Server Error", http.StatusInternalServerError, errState)
			invalidRespFromDestMsg := protocol.TunnelMessage{MsgType: protocol.RESPONSE, MsgData: responseBuff.Bytes()}
			ct.outgoingChannel <- invalidRespFromDestMsg
		}
	}
}

func Run(config ConfigOptions) {
	logger.LogStartMmarServer(config.TcpPort, config.HttpPort)

	// Channel handler for interrupt signal
	sigInt := make(chan os.Signal, 1)
	signal.Notify(sigInt, os.Interrupt)

	mux := http.NewServeMux()

	// Initialize Mmar Server
	mmarServer := MmarServer{
		clients:      map[string]ClientTunnel{},
		tunnelsPerIP: map[string][]string{},
	}
	mux.Handle("/", logger.LoggerMiddleware(&mmarServer))

	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%s", config.TcpPort))
		if err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
			return
		}
		logger.Log(
			constants.DEFAULT_COLOR,
			fmt.Sprintf(
				"TCP Server started successfully!\nListening for TCP Connections on port %s...",
				config.TcpPort,
			),
		)

		for {
			conn, err := ln.Accept()
			if err != nil {
				logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to accept TCP connection: %v", err))
			} else {
				go mmarServer.handleTcpConnection(conn)
			}
		}
	}()

	go func() {
		logger.Log(
			constants.DEFAULT_COLOR,
			fmt.Sprintf(
				"HTTP Server started successfully!\nListening for HTTP Requests on %s...",
				config.HttpPort,
			),
		)
		if err := http.ListenAndServe(fmt.Sprintf(":%s", config.HttpPort), mux); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Error listening and serving: %s\n", err)
		}
	}()

	// Wait for an interrupt signal, if received, terminate gracefully
	<-sigInt
	log.Printf("Gracefully shutting down server...")
}
