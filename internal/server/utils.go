package server

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yusuf-musleh/mmar/constants"
)

var (
	ErrReadBodyChunk                  = errors.New(strings.ToLower(constants.READ_BODY_CHUNK_ERR_TEXT))
	ErrReadBodyChunkTimeout           = errors.New(strings.ToLower(constants.READ_BODY_CHUNK_TIMEOUT_ERR_TEXT))
	ErrClientDisconnected             = errors.New(strings.TrimSuffix(strings.ToLower(constants.CLIENT_DISCONNECT_ERR_TEXT), "."))
	ErrReadRespBody                   = errors.New(strings.TrimSuffix(strings.ToLower(constants.READ_RESP_BODY_ERR_TEXT), "."))
	ErrMaxReqBodySize                 = errors.New(strings.ToLower(constants.MAX_REQ_BODY_SIZE_ERR_TEXT))
	ErrFailedToForwardToMmarClient    = errors.New(strings.ToLower(constants.FAILED_TO_FORWARD_TO_MMAR_CLIENT_ERR_TEXT))
	ErrFailedToReadRespFromMmarClient = errors.New(strings.ToLower(constants.FAILED_TO_READ_RESP_FROM_MMAR_CLIENT_ERR_TEXT))
)

func respondWith(respText string, w http.ResponseWriter, statusCode int) {
	w.Header().Set("Content-Length", strconv.Itoa(len(respText)))
	w.Header().Set("Connection", "close")
	w.WriteHeader(statusCode)
	_, _ = w.Write([]byte(respText))
}

func handleCancel(cause error, w http.ResponseWriter) {
	switch cause {
	case context.Canceled:
		// Cancelled, do nothing
		return
	case ErrReadBodyChunkTimeout:
		respondWith(cause.Error(), w, http.StatusRequestTimeout)
	case ErrReadBodyChunk, ErrClientDisconnected:
		respondWith(cause.Error(), w, http.StatusBadRequest)
	case ErrReadRespBody:
		respondWith(cause.Error(), w, http.StatusInternalServerError)
	case ErrMaxReqBodySize:
		respondWith(cause.Error(), w, http.StatusRequestEntityTooLarge)
	case ErrFailedToForwardToMmarClient, ErrFailedToReadRespFromMmarClient:
		respondWith(cause.Error(), w, http.StatusServiceUnavailable)
	}
}

func cancelRead(ctx context.Context, cancel context.CancelCauseFunc) {
	if errors.Is(ctx.Err(), context.Canceled) {
		// If context is Already cancelled, do nothing
		return
	}

	// Cancel request
	cancel(ErrReadBodyChunkTimeout)
}

// Serialize HTTP request inorder to tunnel it to mmar client
func serializeRequest(ctx context.Context, r *http.Request, cancel context.CancelCauseFunc, serializedRequestChannel chan []byte, maxRequestSize int) {
	var requestBuff bytes.Buffer

	// Writing & serializing the HTTP Request Line
	requestBuff.WriteString(
		fmt.Sprintf(
			"%v %v %v\nHost: %v\n",
			r.Method,
			r.RequestURI,
			r.Proto,
			r.Host,
		),
	)

	// Initialize read buffer/counter
	bufferSize := 2048
	contentLength := 0
	buf := make([]byte, bufferSize)
	reqBodyBytes := []byte{}

	// Keep reading response until completely read
	for {
		// Cancel request if read buffer times out
		readBufferTimeout := time.AfterFunc(
			constants.REQ_BODY_READ_CHUNK_TIMEOUT*time.Second,
			func() { cancelRead(ctx, cancel) },
		)
		r, readErr := r.Body.Read(buf)
		readBufferTimeout.Stop()
		contentLength += r
		if contentLength > maxRequestSize {
			cancel(ErrMaxReqBodySize)
			return
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				reqBodyBytes = append(reqBodyBytes, buf[:r]...)
				break
			}
			// Cancel request if there was an error reading
			cancel(ErrReadBodyChunk)
			return
		}
		reqBodyBytes = append(reqBodyBytes, buf[:r]...)
	}

	// Set actual Content-Length header
	r.Header.Set("Content-Length", strconv.Itoa(contentLength))

	// Serialize headers
	_ = r.Header.Clone().Write(&requestBuff)

	// Add new line
	requestBuff.WriteByte('\n')

	// Write body to buffer
	requestBuff.Write(reqBodyBytes)
	requestBuff.WriteByte('\n')

	// Send serialized request through channel
	serializedRequestChannel <- requestBuff.Bytes()
}

// Create HTTP response sent from mmar server to the end-user client
func createSerializedServerResp(status string, statusCode int, body string) bytes.Buffer {
	resp := http.Response{
		Status:     status,
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}

	// Writing response to buffer to tunnel it back
	var responseBuff bytes.Buffer
	_ = resp.Write(&responseBuff)

	return responseBuff
}

// Generate a random ID from ID_CHARSET of length ID_LENGTH
func GenerateRandomID() string {
	b := make([]byte, constants.ID_LENGTH)
	if _, err := cryptoRand.Read(b); err != nil {
		// Consider a fallback or panic, but for this use case, a panic is acceptable
		panic("failed to generate random bytes for subdomain")
	}

	for i := 0; i < constants.ID_LENGTH; i++ {
		b[i] = constants.ID_CHARSET[int(b[i])%len(constants.ID_CHARSET)]
	}
	return string(b)
}

// Generate a random 32-bit unsigned integer
func GenerateRandomUint32() uint32 {
	var randomUint32 uint32
	_ = binary.Read(cryptoRand.Reader, binary.BigEndian, &randomUint32)
	return randomUint32
}
