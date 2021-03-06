package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type myTransport struct {
	matcher
	limiters
	stats   map[string]MonitoringPath
	statsMu sync.RWMutex
}

type ModifiedRequest struct {
	Path       string
	RemoteAddr string
	ID         json.RawMessage
}

//RPC request
type rpcRequest struct {
	Method string
	ID     json.RawMessage `json:"id,omitempty"`
}

func isBatch(msg []byte) bool {
	for _, c := range msg {
		if c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d {
			continue
		}
		return c == '['
	}
	return false
}

func parseRequests(r *http.Request) []ModifiedRequest {
	var res []ModifiedRequest
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if r.Body != nil {
		bodyBytes, err := ioutil.ReadAll(r.Body)
		r.Body.Close() //closing reader
		if err == nil {
			if isBatch(bodyBytes) {
				var arr []rpcRequest
				err = json.Unmarshal(bodyBytes, &arr)
				if err == nil {
					for _, t := range arr {
						res = append(res, ModifiedRequest{
							ID:         t.ID,
							Path:       t.Method,
							RemoteAddr: ip,
						})
					}
				} else {
					log.Println("cannot parse JSON batch request", "err", err.Error(), r)
				}
			} else {
				var t rpcRequest
				err = json.Unmarshal(bodyBytes, &t)
				if err == nil {
					res = append(res, ModifiedRequest{
						ID:         t.ID,
						Path:       t.Method,
						RemoteAddr: ip,
					})
				} else {
					log.Println("cannot parse JSON single request", "err", err.Error(), r)
				}
			}
		} else {
			log.Println("cannot read body", "err", err.Error(), r)
		}
		r.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
	}
	if len(res) == 0 {
		res = append(res, ModifiedRequest{
			Path:       r.URL.Path,
			RemoteAddr: ip,
		})
	}

	return res
}

func jsonRPCError(id json.RawMessage, jsonCode int, msg string) interface{} {
	type errResponse struct {
		Version string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	resp := errResponse{
		Version: "2.0",
		ID:      id,
	}
	resp.Error.Code = jsonCode
	resp.Error.Message = msg
	return resp
}

func jsonRPCResponse(id json.RawMessage, jsonCode, httpCode int, msg string) (*http.Response, error) {
	body, err := json.Marshal(jsonRPCError(id, jsonCode, msg))
	if err != nil {
		return nil, err
	}
	return &http.Response{
		Body:       ioutil.NopCloser(bytes.NewReader(body)),
		StatusCode: httpCode,
	}, nil
}

func (t *myTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var response *http.Response
	var err error
	start := time.Now()
	parsedRequests := parseRequests(request)

	for _, parsedRequest := range parsedRequests {
		if !t.AllowLimit(parsedRequest) {
			if verboseLogging {
				log.Println("User hit the limit:", parsedRequest.Path, " from IP: ", parsedRequest.RemoteAddr)
			}
			return jsonRPCResponse(parsedRequest.ID, -32000, http.StatusTooManyRequests, "You hit the request limit")
		}

		if !t.MatchAnyRule(parsedRequest) {
			log.Println("Not allowed:", parsedRequest.Path, " from IP: ", parsedRequest.RemoteAddr)
			return jsonRPCResponse(parsedRequest.ID, -32601, http.StatusUnauthorized, "You are not authorized to make this request")
		}
	}
	request.Host = request.RemoteAddr //workaround for CloudFlare
	response, err = http.DefaultTransport.RoundTrip(request)
	if err != nil {
		log.Println("Error response from RoundTrip:", err)
		returnErrorCode := http.StatusInternalServerError
		if response != nil {
			returnErrorCode = response.StatusCode
		}
		return jsonRPCResponse(parsedRequests[0].ID, -32603, returnErrorCode, "Internal error") //returning ID of the first request
	}

	elapsed := time.Since(start)

	for _, parsedRequest := range parsedRequests {
		t.updateStats(parsedRequest, elapsed)
		if verboseLogging {
			log.Println("Response Time:", elapsed.Seconds(), " path: ", parsedRequest.Path, " from IP: ", parsedRequest.RemoteAddr)
		}
	}
	return response, err
}
