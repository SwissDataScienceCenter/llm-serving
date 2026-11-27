// Package service implements the gRPC services served by the extension service.
package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cloudevents "github.com/cloudevents/sdk-go/v2/event"
	envoy_api_v3_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoy_type_v3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	om "github.com/openmeterio/openmeter/api/client/go"
	"github.com/sdsc-vllm/envoy-extension-service/internal/config"
)

const (
	openMeterTimeout = 5 * time.Second
)

type ExternalProcessorMeteringService struct {
	client *om.ClientWithResponses
	cfg    *config.Config
	envoy_service_proc_v3.UnimplementedExternalProcessorServer
}

// Used for storing parsed usage data
type usage struct {
	Id                    string
	Input, Cached, Output int
}

// Used for decoding body json
type bodyEnvelope struct {
	Id    string         `json:"id"`
	Usage *usageEnvelope `json:"usage"`
}

type usageEnvelope struct {
	PromptTokens        int                    `json:"prompt_tokens"`
	CompletionTokens    int                    `json:"completion_tokens"`
	PromptTokensDetails *promptDetailsEnvelope `json:"prompt_tokens_details"`
}

type promptDetailsEnvelope struct {
	CachedTokens int `json:"cached_tokens"`
}

func NewExternalProcessorMeteringService(cfg *config.Config) (*ExternalProcessorMeteringService, error) {
	client, err := om.NewAuthClientWithResponses(cfg.OpenMeter.URL, cfg.OpenMeter.ApiSecret)
	if err != nil {
		return nil, err
	}
	return &ExternalProcessorMeteringService{client: client, cfg: cfg}, nil
}

// Envoy sends http data in chunks, first request headers and body, then response headers and body
// We need to inspect the response body to get the number of tokens used.
// We store request data in variables and on response, we hand of the ProcessServer to dedicated
// parsing methods that extract the usage. So it's like a two-state state machine, first looping
// for request headers, then a separate loop for parsing body json or SSE jsonl (if streaming)
func (s *ExternalProcessorMeteringService) Process(
	srv envoy_service_proc_v3.ExternalProcessor_ProcessServer,
) error {
	ctx := srv.Context()

	var customerId string
	isStream := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		resp := &envoy_service_proc_v3.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *envoy_service_proc_v3.ProcessingRequest_RequestHeaders:
			customerId = ""
			resp = s.handleRequestHeaders(ctx, v.RequestHeaders, &customerId)
		case *envoy_service_proc_v3.ProcessingRequest_ResponseHeaders:
			resp, isStream = s.handleResponseHeaders(v.ResponseHeaders)

			// send response headers response
			if err := srv.Send(resp); err != nil {
				slog.Error("send error", "err", err)
				return err
			}
			var usage *usage
			if isStream {
				// hand off to stream handler
				usage, err = s.handleSSEBody(srv)

			} else {
				// hand off to regular json handler
				usage, err = s.handleJSONBody(srv)
			}
			if err != nil {
				slog.Error("error getting usage from body", "err", err)
			}
			if usage == nil {
				slog.Warn("could not find usage data in request body")
			} else {
				s.ingestUsage(ctx, customerId, *usage)
			}
			continue
		case *envoy_service_proc_v3.ProcessingRequest_ResponseBody:
			// we only reach this if the handoff from handeResponse returned early, so we just continue
			resp = continueBodyResponse()

		default:
			// we got a request type we don't care about, so we just return the empty response
		}

		if err := srv.Send(resp); err != nil {
			slog.Error("send error", "err", err)
			return err
		}
	}
}

func (s *ExternalProcessorMeteringService) handleSSEBody(srv envoy_service_proc_v3.ExternalProcessor_ProcessServer) (*usage, error) {
	r := &bodyReader{srv: srv}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var u *usage
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] == ':' {
			continue // empty or event separator
		}
		data, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue // some weird unhandle SSE event type
		}
		data = bytes.TrimSpace(data)
		if bytes.Equal(data, []byte("[DONE]")) {
			if u == nil {
				return nil, fmt.Errorf("no usage data in body")
			}
			return u, nil // end of SSE events stream, return last captured usage
		}
		var env bodyEnvelope
		if err := json.Unmarshal(data, &env); err == nil {
			usage, err := envelopeToUsage(env)
			if err == nil {
				u = usage
			}
		}

	}
	if u == nil {
		if err := sc.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("no usage data in body")
	}
	return u, nil // end of SSE events stream, return last captured usage

}

func envelopeToUsage(env bodyEnvelope) (*usage, error) {
	if env.Usage == nil {
		return nil, fmt.Errorf("did not find usage data in body")
	}
	ct := 0
	if env.Usage.PromptTokensDetails != nil {
		ct = env.Usage.PromptTokensDetails.CachedTokens
	}
	u := &usage{Id: env.Id, Input: env.Usage.PromptTokens, Cached: ct, Output: env.Usage.CompletionTokens}
	return u, nil
}

func (s *ExternalProcessorMeteringService) handleJSONBody(srv envoy_service_proc_v3.ExternalProcessor_ProcessServer) (*usage, error) {
	r := &bodyReader{srv: srv}
	dec := json.NewDecoder(r)
	var env bodyEnvelope
	if err := dec.Decode(&env); err != nil && err != io.EOF {
		slog.Error("could not parse usage data", "err", err)
		// drain request so it can continue
		io.Copy(io.Discard, r)
		return nil, err
	}
	u, err := envelopeToUsage(env)
	if err != nil {
		slog.Error("could not parse usage data", "err", err)
		// drain request so it can continue
		io.Copy(io.Discard, r)
		return nil, err
	}

	// fully drain reader to force responses to envoy
	// reading the reader causes HTTP chunks to be processed and acknowledge to envoy
	io.Copy(io.Discard, r)
	return u, nil
}

func (s *ExternalProcessorMeteringService) handleResponseHeaders(hh *envoy_service_proc_v3.HttpHeaders) (*envoy_service_proc_v3.ProcessingResponse, bool) {
	isStream := false
	if hh != nil {
		for _, hdr := range hh.Headers.GetHeaders() {
			if strings.ToLower(hdr.Key) == "content-type" && strings.Contains(string(hdr.RawValue), "text/event-stream") {
				isStream = true
			}
		}
	}
	resp := continueHeadersResponse()
	return resp, isStream

}

// check customer entitlement and reject requests if no balance is left
func (s *ExternalProcessorMeteringService) handleRequestHeaders(
	ctx context.Context,
	hh *envoy_service_proc_v3.HttpHeaders,
	customerId *string,
) *envoy_service_proc_v3.ProcessingResponse {
	if hh != nil {
		for _, hdr := range hh.Headers.GetHeaders() {
			if strings.ToLower(hdr.Key) == "x-customer-id" {
				*customerId = string(hdr.RawValue)
			}
		}
	}
	if *customerId == "" {
		slog.Error("customer ID header missing")
		return errorResponse(envoy_type_v3.StatusCode_BadRequest, "customer ID header missing")
	}

	entCtx, cancel := context.WithTimeout(ctx, openMeterTimeout)
	ent, err := s.client.GetCustomerEntitlementValueWithResponse(entCtx, *customerId, s.cfg.OpenMeter.FeatureKey, nil)
	cancel()
	if err != nil {
		slog.Error("couldn't query entitlement", "err", err)
		return errorResponse(envoy_type_v3.StatusCode_InternalServerError, "couldn't query entitlement")
	}
	if ent.StatusCode() == 404 {
		return errorResponse(envoy_type_v3.StatusCode_Forbidden, "no metering entitlement found for user")
	}
	if ent.StatusCode() >= 300 {
		slog.Error("couldn't query entitlement", "status", ent.StatusCode())
		return errorResponse(envoy_type_v3.StatusCode_InternalServerError, "couldn't query entitlement")
	}
	if ent.JSON200 == nil {
		slog.Error("couldn't decode entitlement value")
		return errorResponse(envoy_type_v3.StatusCode_InternalServerError, "couldn't decode entitlement value")
	}
	if !ent.JSON200.HasAccess {
		return errorResponse(envoy_type_v3.StatusCode_Forbidden, "access forbidden")
	}

	resp := continueHeadersRequest()

	// No balance limit configured: nothing to gate on.
	b := ent.JSON200.Balance
	if b == nil {
		return resp
	}
	balance := *b
	total := *ent.JSON200.TotalAvailableGrantAmount

	if balance <= 0.0 {
		return overQuotaResponse(balance, total)
	}
	return resp
}

// send token usage to openmeter
func (s *ExternalProcessorMeteringService) ingestUsage(ctx context.Context, customerId string, usage usage) {

	e := cloudevents.New()
	e.SetID(usage.Id)
	e.SetSource("envoy-ext-proc")
	e.SetType("tokens")
	e.SetSubject(customerId)
	e.SetTime(time.Now())
	if err := e.SetData("application/json", map[string]string{
		"input_tokens":        strconv.Itoa(usage.Input),
		"output_tokens":       strconv.Itoa(usage.Output),
		"cached_input_tokens": strconv.Itoa(usage.Cached),
	}); err != nil {
		slog.Error("could not set cloud events data", "err", err)
		return
	}

	ingCtx, cancel := context.WithTimeout(ctx, openMeterTimeout)
	resp, err := s.client.IngestEventWithResponse(ingCtx, e)
	cancel()
	if err != nil {
		slog.Error("could not send usage data to openmeter", "err", err)
	}
	if resp.StatusCode() >= 400 {
		slog.Error("could not send usage data to open meter, got error status", "status", resp.StatusCode())
	}
}

func continueHeadersRequest() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &envoy_service_proc_v3.HeadersResponse{
				Response: &envoy_service_proc_v3.CommonResponse{
					Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

func continueHeadersResponse() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &envoy_service_proc_v3.HeadersResponse{
				Response: &envoy_service_proc_v3.CommonResponse{
					Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

func continueBodyResponse() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ResponseBody{
			ResponseBody: &envoy_service_proc_v3.BodyResponse{
				Response: &envoy_service_proc_v3.CommonResponse{
					Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

func overQuotaResponse(balance, total float64) *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &envoy_service_proc_v3.ImmediateResponse{
				Status: &envoy_type_v3.HttpStatus{
					Code: envoy_type_v3.StatusCode_TooManyRequests,
				},
				Headers: &envoy_service_proc_v3.HeaderMutation{
					SetHeaders: []*envoy_api_v3_core.HeaderValueOption{
						{
							Header: &envoy_api_v3_core.HeaderValue{
								Key:      "x-ratelimit-limit-tokens",
								RawValue: []byte(strconv.FormatFloat(total, 'f', 2, 32)),
							},
							AppendAction: envoy_api_v3_core.HeaderValueOption_OVERWRITE_IF_EXISTS,
						},
						{
							Header: &envoy_api_v3_core.HeaderValue{
								Key:      "x-ratelimit-remaining-tokens",
								RawValue: []byte(strconv.FormatFloat(balance, 'f', 2, 32)),
							},
							AppendAction: envoy_api_v3_core.HeaderValueOption_OVERWRITE_IF_EXISTS,
						},
					},
				},
				Body: []byte("over quota"),
			},
		},
	}
}

func errorResponse(code envoy_type_v3.StatusCode, msg string) *envoy_service_proc_v3.ProcessingResponse {
	resp := &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &envoy_service_proc_v3.ImmediateResponse{
				Status: &envoy_type_v3.HttpStatus{
					Code: code,
				},
				Body: []byte(msg),
			},
		},
	}
	return resp
}

type bodyReader struct {
	srv     envoy_service_proc_v3.ExternalProcessor_ProcessServer
	pending []byte
	eos     bool
}

func (r *bodyReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		// loop until we have gathered some pending data
		if r.eos {
			return 0, io.EOF // we reached the end of the body stream
		}

		req, err := r.srv.Recv()
		if err != nil {
			return 0, err
		}
		switch v := req.Request.(type) {
		case *envoy_service_proc_v3.ProcessingRequest_ResponseBody:
			r.pending = v.ResponseBody.Body
			r.eos = v.ResponseBody.EndOfStream

			// reply to upstream
			resp := continueBodyResponse()
			if err := r.srv.Send(resp); err != nil {
				slog.Error("send error", "err", err)
				return 0, err
			}
		default:
			slog.Error("got message of unexpected type while parsing response body", "type", v)
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
