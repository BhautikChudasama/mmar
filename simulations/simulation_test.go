package simulations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuf-musleh/mmar/constants"
	"github.com/yusuf-musleh/mmar/simulations/devserver"
	"github.com/yusuf-musleh/mmar/simulations/dnsserver"
)

func StartMmarServer(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "./mmar", "server")

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Cancel = func() error {
		fmt.Println("cancelled, server")
		wait := time.NewTimer(4 * time.Second)
		<-wait.C
		return cmd.Process.Signal(os.Interrupt)
	}

	err := cmd.Run()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func StartMmarClient(
	ctx context.Context,
	urlCh chan string,
	localDevServerPort string,
	localDevServerHost string,
	localDevServerProto string,
	customDns string,
	customCert string,
) {
	cmd := exec.CommandContext(
		ctx,
		"./mmar",
		"client",
		"--tunnel-host",
		"localhost",
		"--local-port",
		localDevServerPort,
	)

	if localDevServerHost != "" {
		cmd.Args = append(cmd.Args, "--local-host", localDevServerHost)
	}

	if localDevServerProto != "" {
		cmd.Args = append(cmd.Args, "--local-proto", localDevServerProto)
	}

	if customDns != "" {
		cmd.Args = append(cmd.Args, "--custom-dns", customDns)
	}

	if customCert != "" {
		cmd.Args = append(cmd.Args, "--custom-cert", customCert)
	}

	cmd.Args = append(cmd.Args, "")

	cmd.Stdout = os.Stdout

	// Pipe Stderr To capture logs for extracting the tunnel url
	pipe, _ := cmd.StderrPipe()

	cmd.Cancel = func() error {
		fmt.Println("cancelled, client")
		return cmd.Process.Signal(os.Interrupt)
	}

	err := cmd.Start()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal("Failed to start", err)
	}

	// Read through the logs (stderr), print them and extract the tunnel url
	// to send back through the channel
	go func() {
		stdoutReader := bufio.NewReader(pipe)
		line, readErr := stdoutReader.ReadString('\n')
		for readErr == nil {
			fmt.Print(line)
			tunnelUrl := extractTunnelURL(line)
			if tunnelUrl != "" {
				urlCh <- tunnelUrl
			}
			line, readErr = stdoutReader.ReadString('\n')
		}
		// Print extra line at the end
		fmt.Println()
	}()

	waitErr := cmd.Wait()
	if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
		log.Fatal("Failed to wait", waitErr)
	}
}

func StartLocalDevServer(proto string, addr string) *devserver.DevServer {
	ds := devserver.NewDevServer(proto, addr)
	log.Printf("Started local dev server on: %v://%v:%v", proto, addr, ds.Port())
	return ds
}

// Test to verify successful GET request through mmar tunnel returned expected request/response
func verifyGetRequestSuccess(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	req, reqErr := http.NewRequest("GET", tunnelUrl+devserver.GET_SUCCESS_URL, nil)
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-get-request-success")

	// Adding query params to confirm that they get propogated when going through mmar
	q := req.URL.Query()
	q.Add("first", "query param")
	q.Add("second", "param & last")
	req.URL.RawQuery = q.Encode()

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-get-request-success"},
	}

	expectedBody := map[string]interface{}{
		"success": true,
		"data":    "some data",
		"echo": map[string]interface{}{
			"reqHeaders":     expectedReqHeaders,
			"reqQueryParams": q,
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-get",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyGetRequestSuccess")
}

// Test to verify failed GET request through mmar tunnel returned expected request/response
func verifyGetRequestFail(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	req, reqErr := http.NewRequest("GET", tunnelUrl+devserver.GET_FAILURE_URL, nil)
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-get-request-fail")

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-get-request-fail"},
	}

	expectedBody := map[string]interface{}{
		"success": false,
		"error":   "Sent bad GET request",
		"echo": map[string]interface{}{
			"reqHeaders": expectedReqHeaders,
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusBadRequest,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-get-fail",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyGetRequestFail")
}

// Test to verify successful POST request through mmar tunnel returned expected request/response
func verifyPostRequestSuccess(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	reqBody := map[string]interface{}{
		"success": true,
		"payload": map[string]interface{}{
			"some":     "data",
			"moreData": 123,
		},
	}
	serializedReqBody, _ := json.Marshal(reqBody)
	req, reqErr := http.NewRequest("POST", tunnelUrl+devserver.POST_SUCCESS_URL, bytes.NewBuffer(serializedReqBody))
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-post-request-success")

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-post-request-success"},
		"Content-Length":  {strconv.Itoa(len(serializedReqBody))},
	}

	expectedBody := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"posted": "data",
		},
		"echo": map[string]interface{}{
			"reqHeaders": expectedReqHeaders,
			"reqBody":    reqBody,
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-post-success",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyPostRequestSuccess")
}

// Test to verify failed POST request through mmar tunnel returned expected request/response
func verifyPostRequestFail(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	reqBody := map[string]interface{}{
		"success": false,
		"payload": map[string]interface{}{
			"some":     "data",
			"moreData": 123,
		},
	}
	serializedReqBody, _ := json.Marshal(reqBody)
	req, reqErr := http.NewRequest("POST", tunnelUrl+devserver.POST_FAILURE_URL, bytes.NewBuffer(serializedReqBody))
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-post-request-fail")

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-post-request-fail"},
		"Content-Length":  {strconv.Itoa(len(serializedReqBody))},
	}

	expectedBody := map[string]interface{}{
		"success": false,
		"error":   "Sent bad POST request",
		"echo": map[string]interface{}{
			"reqHeaders": expectedReqHeaders,
			"reqBody":    reqBody,
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusBadRequest,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-post-fail",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyPostRequestFail")
}

// Test to verify redirects works as expected
func verifyRedirectsHandled(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	reqBody := map[string]interface{}{
		"success": true,
		"payload": map[string]interface{}{
			"some":     "data",
			"moreData": 123,
		},
	}
	serializedReqBody, _ := json.Marshal(reqBody)
	req, reqErr := http.NewRequest("POST", tunnelUrl+devserver.REDIRECT_URL, bytes.NewBuffer(serializedReqBody))
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}

	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-redirect-request")

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-redirect-request"},
		"Referer":         {tunnelUrl + "/redirect"}, // Include referer header since it redirects
	}

	expectedBody := map[string]interface{}{
		"success": true,
		"data":    "some data",
		"echo": map[string]interface{}{
			"reqHeaders":     expectedReqHeaders,
			"reqQueryParams": map[string][]string{},
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-get",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyRedirectsHandled")
}

// Test to verify a HTTP request with an invalid method is handled
func verifyInvalidMethodRequestHandled(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	url, urlErr := url.Parse(tunnelUrl + devserver.GET_SUCCESS_URL)
	if urlErr != nil {
		log.Fatal("Failed to create url", urlErr)
	}

	req := &http.Request{
		Method: "INVALID_METHOD",
		URL:    url,
		Header: make(http.Header),
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-invalid-method-request")

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response %v", respErr)
	}

	// Using the same validation for "verifyGetRequestSuccess" since we are
	// hitting the same endpoint
	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-invalid-method-request"},
	}

	expectedBody := map[string]interface{}{
		"success": true,
		"data":    "some data",
		"echo": map[string]interface{}{
			"reqHeaders":     expectedReqHeaders,
			"reqQueryParams": map[string][]string{},
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-get",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyInvalidMethodRequestHandled")
}

// Test to verify a HTTP request with invalid headers is handled
func verifyInvalidHeadersRequestHandled(t *testing.T, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	dialUrl := strings.Replace(tunnelUrl, "http://", "", 1)

	// Write a raw HTTP request with an invalid header
	req := "GET / HTTP/1.1\r\n" +
		"Host: " + dialUrl + "\r\n" +
		":Invalid-Header: :value\r\n" + // Header that starts with a colon (invalid)
		"\r\n"

	// Manually perform request and read response
	conn := manualHttpRequest(dialUrl, req)
	resp, respErr := manualReadResponse(conn)

	if respErr != nil {
		t.Errorf("%v: Failed to get response %v", "verifyInvalidHeadersRequestHandled", respErr)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf(
			"%v: resp.StatusCode = %v; want %v",
			"verifyInvalidHeadersRequestHandled",
			resp.StatusCode,
			http.StatusBadRequest,
		)
	}
}

// Test to verify a HTTP request with invalid protocol version is handled
func verifyInvalidHttpVersionRequestHandled(t *testing.T, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	dialUrl := strings.Replace(tunnelUrl, "http://", "", 1)

	// Write a raw HTTP request with an invalid HTTP version
	req := "GET / HTTP/2.0.1\r\n" + // Invalid HTTP version
		"Host: " + dialUrl + "\r\n" +
		"\r\n"

	// Manually perform request and read response
	conn := manualHttpRequest(dialUrl, req)
	resp, respErr := manualReadResponse(conn)

	if respErr != nil {
		t.Errorf("%v: Failed to get response %v", "verifyInvalidHttpVersionRequestHandled", respErr)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf(
			"%v: resp.StatusCode = %v; want %v",
			"verifyInvalidHttpVersionRequestHandled",
			resp.StatusCode,
			http.StatusBadRequest,
		)
	}
}

// Test to verify a HTTP request with a invalid Content-Length header
func verifyInvalidContentLengthRequestHandled(t *testing.T, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	dialUrl := strings.Replace(tunnelUrl, "http://", "", 1)

	// Write a raw HTTP request with an invalid Content-Length header
	req := "POST /post HTTP/1.1\r\n" +
		"Host: " + dialUrl + "\r\n" +
		"Content-Length: abc" + "\r\n" + // Invalid Content-Length header
		"\r\n"

	// Manually perform request and read response for invalid Content-Length
	conn := manualHttpRequest(dialUrl, req)
	resp, respErr := manualReadResponse(conn)

	if respErr != nil {
		t.Errorf("%v: Failed to get response %v", "verifyInvalidContentLengthRequestHandled", respErr)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf(
			"%v: resp.StatusCode = %v; want %v",
			"verifyInvalidContentLengthRequestHandled",
			resp.StatusCode,
			http.StatusBadRequest,
		)
	}
}

// Test to verify a HTTP request with a mismatched Content-Length header
func verifyMismatchedContentLengthRequestHandled(t *testing.T, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	dialUrl := strings.Replace(tunnelUrl, "http://", "", 1)

	serializedBody := "{\"message\": \"Hello\"}\r\n"

	// Write a raw HTTP request with an invalid Content-Length header
	req := "POST /post HTTP/1.1\r\n" +
		"Host: " + dialUrl + "\r\n" +
		"Content-Length: 25\r\n" + // Mismatched Content-Length header, should be 20
		"Simulation-Test: verify-mismatched-content-length-request-handled\r\n" +
		"\r\n" +
		serializedBody

	// Manually perform request and read response for mismatched content-length
	conn := manualHttpRequest(dialUrl, req)
	resp, respErr := manualReadResponse(conn)

	if respErr != nil {
		t.Errorf("%v: Failed to get response %v", "verifyMismatchedContentLengthRequestHandled", respErr)
	}

	expectedBody := constants.READ_BODY_CHUNK_TIMEOUT_ERR_TEXT

	expectedResp := expectedResponse{
		statusCode: http.StatusRequestTimeout,
		headers: map[string]string{
			"Content-Length": strconv.Itoa(len(expectedBody)),
			"Content-Type":   "text/plain; charset=utf-8",
		},
		textBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyMismatchedContentLengthRequestHandled")
}

// Test to verify a HTTP request with a Content-Length header but no body
func verifyContentLengthWithNoBodyRequestHandled(t *testing.T, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	dialUrl := strings.Replace(tunnelUrl, "http://", "", 1)

	// Write a raw HTTP request with Content-Length header but no body
	req := "POST /post HTTP/1.1\r\n" +
		"Host: " + dialUrl + "\r\n" +
		"Content-Length: 25\r\n" + // Content-Length header provided but no body
		"Simulation-Test: verify-content-length-with-no-body-request-handled\r\n" +
		"\r\n"

	// Manually perform request and read response
	conn := manualHttpRequest(dialUrl, req)
	resp, respErr := manualReadResponse(conn)

	if respErr != nil {
		t.Errorf("%v: Failed to get response %v", "verifyContentLengthWithNoBodyRequestHandled", respErr)
	}
	expectedBody := constants.READ_BODY_CHUNK_TIMEOUT_ERR_TEXT

	expectedResp := expectedResponse{
		statusCode: http.StatusRequestTimeout,
		headers: map[string]string{
			"Content-Length": strconv.Itoa(len(expectedBody)),
			"Content-Type":   "text/plain; charset=utf-8",
		},
		textBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyContentLengthWithNoBodyRequestHandled")
}

// Test to verify a HTTP request with a large body but still within the limit
func verifyRequestWithLargeBody(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	littleUnderTenMb := 999989
	reqBody := map[string]interface{}{
		"success": true,
		"payload": make([]byte, littleUnderTenMb),
	}

	serializedReqBody, _ := json.Marshal(reqBody)
	req, reqErr := http.NewRequest("POST", tunnelUrl+devserver.POST_SUCCESS_URL, bytes.NewBuffer(serializedReqBody))
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-large-post-request-success")

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedReqHeaders := map[string][]string{
		"User-Agent":      {"Go-http-client/1.1"}, // Default header in golang client
		"Accept-Encoding": {"gzip"},               // Default header in golang client
		"Connection":      {"close"},
		"Simulation-Test": {"verify-large-post-request-success"},
		"Content-Length":  {strconv.Itoa(len(serializedReqBody))},
	}

	expectedBody := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"posted": "data",
		},
		"echo": map[string]interface{}{
			"reqHeaders": expectedReqHeaders,
			"reqBody":    reqBody,
		},
	}
	marshaledBody, _ := json.Marshal(expectedBody)

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length":    strconv.Itoa(len(marshaledBody)),
			"Content-Type":      "application/json",
			"Simulation-Header": "devserver-handle-post-success",
		},
		jsonBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyRequestWithLargeBody")
}

// Test to verify a HTTP request with a very large body, over the 10mb limit
func verifyRequestWithVeryLargeBody(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	hundredMb := 100000000
	reqBody := map[string]interface{}{
		"success": true,
		"payload": make([]byte, hundredMb),
	}

	serializedReqBody, _ := json.Marshal(reqBody)
	req, reqErr := http.NewRequest("POST", tunnelUrl+devserver.POST_SUCCESS_URL, bytes.NewBuffer(serializedReqBody))
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}
	// Adding custom header to confirm that they are propogated when going through mmar
	req.Header.Set("Simulation-Test", "verify-very-large-post-request-success")

	resp, respErr := client.Do(req)
	if respErr != nil {
		// Check if connection was closed in the middle of writing, that's also valid behavior
		if !strings.Contains(respErr.Error(), "write: connection reset by peer") {
			t.Errorf("Failed to get response: %v", respErr)
		}
		return
	}

	expectedBody := constants.MAX_REQ_BODY_SIZE_ERR_TEXT

	expectedResp := expectedResponse{
		statusCode: http.StatusRequestEntityTooLarge,
		headers: map[string]string{
			"Content-Length": strconv.Itoa(len(expectedBody)),
			"Content-Type":   "text/plain; charset=utf-8",
		},
		textBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyRequestWithVeryLargeBody")
}

// Test to verify that mmar handles invalid response from dev server gracefully
func verifyDevServerReturningInvalidRespHandled(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	req, reqErr := http.NewRequest("GET", tunnelUrl+devserver.BAD_RESPONSE_URL, nil)
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedBody := constants.READ_RESP_BODY_ERR_TEXT

	expectedResp := expectedResponse{
		statusCode: http.StatusInternalServerError,
		headers: map[string]string{
			"Content-Length": strconv.Itoa(len(expectedBody)),
			"Content-Type":   "text/plain; charset=utf-8",
		},
		textBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyDevServerReturningInvalidRespHandled")
}

// Test to verify that mmar timesout if devserver takes too long to respond
func verifyDevServerLongRunningReqHandledGradefully(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	req, reqErr := http.NewRequest("GET", tunnelUrl+devserver.LONG_RUNNING_URL, nil)
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedBody := constants.DEST_REQUEST_TIMEDOUT_ERR_TEXT

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length": strconv.Itoa(len(expectedBody)),
			"Content-Type":   "text/plain; charset=utf-8",
		},
		textBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyDevServerLongRunningReqHandledGradefully")
}

// Test to verify that mmar handles crashes in the devserver gracefully
func verifyDevServerCrashHandledGracefully(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup) {
	defer wg.Done()
	req, reqErr := http.NewRequest("GET", tunnelUrl+devserver.CRASH_URL, nil)
	if reqErr != nil {
		log.Fatalf("Failed to create new request: %v", reqErr)
	}

	resp, respErr := client.Do(req)
	if respErr != nil {
		t.Errorf("Failed to get response: %v", respErr)
	}

	expectedBody := constants.LOCALHOST_NOT_RUNNING_ERR_TEXT

	expectedResp := expectedResponse{
		statusCode: http.StatusOK,
		headers: map[string]string{
			"Content-Length": strconv.Itoa(len(expectedBody)),
			"Content-Type":   "text/plain; charset=utf-8",
		},
		textBody: expectedBody,
	}

	validateRequestResponse(t, expectedResp, resp, "verifyDevServerCrashHandledGracefully")
}

func TestSimulation(t *testing.T) {
	simulationCtx, simulationCancel := context.WithCancel(context.Background())

	// Start a local dev server with http
	localDevServer := StartLocalDevServer("http", "localhost")
	defer localDevServer.Close()

	// Start a local dev server with https
	localDevTLSServer := StartLocalDevServer("https", "example.com")
	defer localDevTLSServer.Close()

	// Write cert to file so we are able to pass it into mmar client
	certErr := os.WriteFile("./temp-cert", localDevTLSServer.Certificate().Raw, 0644) // 0644 is file permissions
	if certErr != nil {
		log.Fatal(certErr)
	}

	go dnsserver.StartDnsServer()

	go StartMmarServer(simulationCtx)
	wait := time.NewTimer(2 * time.Second)
	<-wait.C

	// Start a basic mmar client
	basicClientUrlCh := make(chan string)
	go StartMmarClient(simulationCtx, basicClientUrlCh, localDevServer.Port(), "", "", "", "")

	// Start another basic mmar client
	basicClientUrlCh2 := make(chan string)
	go StartMmarClient(simulationCtx, basicClientUrlCh2, localDevServer.Port(), "", "", "", "")

	// Wait for all tunnel urls
	mmarClientsCount := 2
	tunnelUrls := []string{}
	for range mmarClientsCount {
		select {
		case tunnelUrl := <-basicClientUrlCh:
			tunnelUrls = append(tunnelUrls, tunnelUrl)
		case tunnelUrl := <-basicClientUrlCh2:
			tunnelUrls = append(tunnelUrls, tunnelUrl)
		}
	}

	// Initialize http client
	client := httpClient()

	var wg sync.WaitGroup

	simulationTests := []func(t *testing.T, client *http.Client, tunnelUrl string, wg *sync.WaitGroup){
		// Perform simulated usage tests
		verifyGetRequestSuccess,
		verifyGetRequestFail,
		verifyPostRequestSuccess,
		verifyPostRequestFail,
		verifyRedirectsHandled,

		// Perform Invalid HTTP requests to test durability of mmar
		verifyInvalidMethodRequestHandled,
		verifyRequestWithLargeBody,

		// Perform edge case usage tests
		verifyRequestWithVeryLargeBody,
		verifyDevServerReturningInvalidRespHandled,
		verifyDevServerLongRunningReqHandledGradefully,
		verifyDevServerCrashHandledGracefully,
	}

	// Tests that require more control hence don't use the built in go http.client
	manualClientSimulationTests := []func(t *testing.T, tunnelUrl string, wg *sync.WaitGroup){
		// Perform Invalid HTTP requests to test durability of mmar
		verifyInvalidHeadersRequestHandled,
		verifyInvalidHttpVersionRequestHandled,
		verifyInvalidContentLengthRequestHandled,
		verifyMismatchedContentLengthRequestHandled,
		verifyContentLengthWithNoBodyRequestHandled,
	}

	// Loop through all tunnel urls and run simulation tests
	for _, tunnelUrl := range tunnelUrls {

		for _, simTest := range simulationTests {
			wg.Add(1)
			go simTest(t, client, tunnelUrl, &wg)
		}

		for _, manualClientSimTest := range manualClientSimulationTests {
			wg.Add(1)
			go manualClientSimTest(t, tunnelUrl, &wg)
		}
	}

	wg.Wait()

	// Delete cert file
	if rmErr := os.Remove("./temp-cert"); rmErr != nil {
		log.Fatal(rmErr)
	}

	// Stop simulation tests
	simulationCancel()

	wait.Reset(6 * time.Second)
	<-wait.C
}
