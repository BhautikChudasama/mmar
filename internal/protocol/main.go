package protocol

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/yusuf-musleh/mmar/constants"
	"github.com/yusuf-musleh/mmar/internal/logger"
)

const (
	REQUEST = uint8(iota + 1)
	RESPONSE
	CREATE_TUNNEL
	RECLAIM_TUNNEL
	TUNNEL_CREATED
	TUNNEL_RECLAIMED
	CLIENT_DISCONNECT
	CLIENT_TUNNEL_LIMIT
	LOCALHOST_NOT_RUNNING
	DEST_REQUEST_TIMEDOUT
	HEARTBEAT_FROM_CLIENT
	HEARTBEAT_FROM_SERVER
	HEARTBEAT_ACK
	INVALID_RESP_FROM_DEST
	INVALID_SUBDOMAIN_NAME
	SUBDOMAIN_ALREADY_TAKEN
	AUTH_TOKEN_REQUIRED
	AUTH_TOKEN_INVALID
	AUTH_TOKEN_LIMIT_EXCEEDED
	// Streaming message types
	REQUEST_STREAM_START
	REQUEST_STREAM_DATA
	REQUEST_STREAM_END
	RESPONSE_STREAM_START
	RESPONSE_STREAM_DATA
	RESPONSE_STREAM_END
)

var (
	ErrInvalidMessageProtocolVersion = errors.New("invalid message protocol version")
	ErrInvalidMessageType            = errors.New("invalid tunnel message type")
)

func isValidTunnelMessageType(mt uint8) (uint8, error) {
	// Iterate through all the message type, from first to last, checking
	// if the provided message type matches one of them
	for msgType := REQUEST; msgType <= RESPONSE_STREAM_END; msgType++ {
		if mt == msgType {
			return msgType, nil
		}
	}

	return 0, ErrInvalidMessageType
}

func TunnelErrState(errState uint8) string {
	// TODO: Have nicer/more elaborative error messages/pages
	errStates := map[uint8]string{
		CLIENT_DISCONNECT:         constants.CLIENT_DISCONNECT_ERR_TEXT,
		LOCALHOST_NOT_RUNNING:     constants.LOCALHOST_NOT_RUNNING_ERR_TEXT,
		DEST_REQUEST_TIMEDOUT:     constants.DEST_REQUEST_TIMEDOUT_ERR_TEXT,
		INVALID_RESP_FROM_DEST:    constants.READ_RESP_BODY_ERR_TEXT,
		INVALID_SUBDOMAIN_NAME:    constants.INVALID_SUBDOMAIN_NAME_ERR_TEXT,
		SUBDOMAIN_ALREADY_TAKEN:   constants.SUBDOMAIN_ALREADY_TAKEN_ERR_TEXT,
		AUTH_TOKEN_REQUIRED:       constants.AUTH_TOKEN_REQUIRED_ERR_TEXT,
		AUTH_TOKEN_INVALID:        constants.AUTH_TOKEN_INVALID_ERR_TEXT,
		AUTH_TOKEN_LIMIT_EXCEEDED: constants.AUTH_TOKEN_LIMIT_EXCEEDED_ERR_TEXT,
	}
	fallbackErr := "An error occured while attempting to tunnel."

	tunnelErr, ok := errStates[errState]
	if !ok {
		tunnelErr = fallbackErr
	}
	return tunnelErr
}

func RespondTunnelErr(errState uint8, w http.ResponseWriter) {
	errBody := TunnelErrState(errState)

	w.Header().Set("Content-Length", strconv.Itoa(len(errBody)))
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(errBody))
}

type Tunnel struct {
	Id        string
	Conn      net.Conn
	CreatedOn time.Time
	Reader    *bufio.Reader
}

type TunnelInterface interface {
	ProcessTunnelMessages(ctx context.Context)
}

type TunnelMessage struct {
	MsgType uint8
	MsgData []byte
}

// A TunnelMessage is serialized in the following format:
//
// +---------+------------+---------------------+------------+-------------------------+
// | Version | Msg Type   | Length of Msg Data  | Delimiter  | Message Data            |
// | (1 byte)| (1 byte)   | (1 or more bytes)   | (1 byte)   | (Variable Length)       |
// +---------+------------+---------------------+------------+-------------------------+
func (tm *TunnelMessage) SerializeMessage() ([]byte, error) {
	serializedMsg := [][]byte{}

	// Determine and validate message type to add prefix
	msgType, err := isValidTunnelMessageType(tm.MsgType)
	if err != nil {
		logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Invalid TunnelMessage type: %v:", tm.MsgType))
		return []byte{}, err
	}

	// Add version of TunnelMessage protocol and TunnelMessage type
	serializedMsg = append(
		serializedMsg,
		[]byte{byte(constants.TUNNEL_MESSAGE_PROTOCOL_VERSION), byte(msgType)},
	)

	// Add message data bytes length
	serializedMsg = append(serializedMsg, []byte(strconv.Itoa(len(tm.MsgData))))

	// Add delimiter to know where the data content starts in the message
	serializedMsg = append(serializedMsg, []byte{byte(constants.TUNNEL_MESSAGE_DATA_DELIMITER)})

	// Add the message data
	serializedMsg = append(serializedMsg, tm.MsgData)

	// Combine all the data with no separators
	return bytes.Join(serializedMsg, nil), nil
}

func (tm *TunnelMessage) readMessageData(length int, reader *bufio.Reader) ([]byte, error) {
	msgData := make([]byte, length)

	if _, err := io.ReadFull(reader, msgData); err != nil {
		logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Failed to read all Msg Data: %v", err))
		return []byte{}, err
	}

	return msgData, nil
}

func (tm *TunnelMessage) deserializeMessage(reader *bufio.Reader) error {
	msgProtocolVersion, err := reader.ReadByte()
	if err != nil {
		return err
	}

	// Check if the message protocol version is correct
	if uint8(msgProtocolVersion) != constants.TUNNEL_MESSAGE_PROTOCOL_VERSION {
		return ErrInvalidMessageProtocolVersion
	}

	msgPrefix, err := reader.ReadByte()
	if err != nil {
		return err
	}

	msgType, err := isValidTunnelMessageType(msgPrefix)
	if err != nil {
		logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Invalid TunnelMessage prefix: %v", msgPrefix))
		return err
	}

	msgLengthStr, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	// Determine the length of the data by stripping out the '\n' and convert to int
	msgLength, err := strconv.Atoi(msgLengthStr[:len(msgLengthStr)-1])
	if err != nil {
		logger.Log(constants.DEFAULT_COLOR, fmt.Sprintf("Could not parse message length: %v", msgLengthStr))
		return err
	}

	// Validate message length to prevent DoS attacks
	const maxMessageSize = constants.MAX_REQ_BODY_SIZE + 16*1024 // 10MB + 16KB overhead
	if msgLength < 0 || msgLength > maxMessageSize {
		return fmt.Errorf("message length %d is invalid or exceeds maximum allowed size", msgLength)
	}

	msgData, readErr := tm.readMessageData(msgLength, reader)
	if readErr != nil {
		return readErr
	}

	tm.MsgType = msgType
	tm.MsgData = msgData

	return nil
}

func (t *Tunnel) ReservedSubdomain() bool {
	return t.Id != ""
}

func (t *Tunnel) SendMessage(tunnelMsg TunnelMessage) error {
	// Serialize tunnel message data
	serializedMsg, serializeErr := tunnelMsg.SerializeMessage()
	if serializeErr != nil {
		return serializeErr
	}
	_, err := t.Conn.Write(serializedMsg)
	return err
}

func (t *Tunnel) ReceiveMessage() (TunnelMessage, error) {
	// Read and deserialize tunnel message data
	tunnelMessage := TunnelMessage{}
	deserializeErr := tunnelMessage.deserializeMessage(t.Reader)

	return tunnelMessage, deserializeErr
}
