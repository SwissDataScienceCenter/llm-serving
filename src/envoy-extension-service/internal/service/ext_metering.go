// Package service implements the gRPC services served by the extension service.
package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
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

type ExternalProcessorMeteringService struct {
	client *om.ClientWithResponses
	cfg    *config.Config
	envoy_service_proc_v3.UnimplementedExternalProcessorServer
}

const openMeterTimeout = 5 * time.Second

func NewExternalProcessorMeteringService(cfg *config.Config) (*ExternalProcessorMeteringService, error) {
	client, err := om.NewAuthClientWithResponses(cfg.OpenMeter.URL, cfg.OpenMeter.ApiSecret)
	if err != nil {
		return nil, err
	}
	return &ExternalProcessorMeteringService{client: client, cfg: cfg}, nil
}

func (s *ExternalProcessorMeteringService) Process(
	srv envoy_service_proc_v3.ExternalProcessor_ProcessServer,
) error {
	ctx := srv.Context()

	// customerId is captured from the request headers and reused in later phases (e.g. ResponseBody).
	var customerId string

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
			// check quota and reject request if there is no remainig balance
			customerId = ""
			if v.RequestHeaders != nil {
				hdrs := v.RequestHeaders.Headers.GetHeaders()
				for _, hdr := range hdrs {
					if hdr.Key == "x-customer-id" {
						customerId = string(hdr.RawValue)
					}
				}
			}
			if customerId == "" {
				slog.Error("customer ID header missing")
				resp = errorResponse(envoy_type_v3.StatusCode_BadRequest, "customer ID header missing")
				break
			}

			entCtx, cancel := context.WithTimeout(ctx, openMeterTimeout)
			ent, err := s.client.GetCustomerEntitlementValueWithResponse(entCtx, customerId, s.cfg.OpenMeter.FeatureKey, nil)
			cancel()
			if err != nil {
				slog.Error("couldn't query entitlement", "err", err)
				resp = errorResponse(envoy_type_v3.StatusCode_InternalServerError, "couldn't query entitlement")
				break
			}
			if ent.StatusCode() == 404 {
				resp = errorResponse(envoy_type_v3.StatusCode_Forbidden, "no metering entitlement found for user")
				break
			}
			if ent.StatusCode() >= 300 {
				slog.Error("couldn't query entitlement", "status", ent.StatusCode)
				resp = errorResponse(envoy_type_v3.StatusCode_InternalServerError, "couldn't query entitlement")
				break
			}
			if ent.JSON200 == nil {
				slog.Error("couldn't decode entitlement value")
				resp = errorResponse(envoy_type_v3.StatusCode_InternalServerError, "couldn't decode entitlement value")
				break

			}
			hasAccess := ent.JSON200.HasAccess
			if !hasAccess {
				resp = errorResponse(envoy_type_v3.StatusCode_Forbidden, "access forbidden")
				break
			}

			resp = &envoy_service_proc_v3.ProcessingResponse{
				Response: &envoy_service_proc_v3.ProcessingResponse_RequestHeaders{
					RequestHeaders: &envoy_service_proc_v3.HeadersResponse{
						Response: &envoy_service_proc_v3.CommonResponse{
							Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
						},
					},
				},
			}

			b := ent.JSON200.Balance
			if b == nil {
				// no balance limit set
				break
			}
			balance := *b
			total := *ent.JSON200.TotalAvailableGrantAmount

			if balance <= 0.0 {
				resp = &envoy_service_proc_v3.ProcessingResponse{
					Response: &envoy_service_proc_v3.ProcessingResponse_ImmediateResponse{
						ImmediateResponse: &envoy_service_proc_v3.ImmediateResponse{
							Status: &envoy_type_v3.HttpStatus{
								Code: envoy_type_v3.StatusCode_PaymentRequired,
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

		case *envoy_service_proc_v3.ProcessingRequest_ResponseBody:
			// update quota with used tokens
			var body map[string]any
			resp = &envoy_service_proc_v3.ProcessingResponse{
				Response: &envoy_service_proc_v3.ProcessingResponse_ResponseBody{
					ResponseBody: &envoy_service_proc_v3.BodyResponse{
						Response: &envoy_service_proc_v3.CommonResponse{
							Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
						},
					},
				},
			}
			if v.ResponseBody == nil {
				break
			}
			err := json.Unmarshal(v.ResponseBody.Body, &body)
			if err != nil {
				// can't parse body as json, must be something else, ignore
				break
			}

			if usage, ok := body["usage"].(map[string]any); ok {
				it, ok := usage["prompt_tokens"].(float64)
				if !ok {
					slog.Warn("could not fetch input tokens from response usage")
				}
				input_tokens := int(it)

				ot, ok := usage["completion_tokens"].(float64)
				if !ok {
					slog.Warn("could not fetch output tokens from response usage")
				}
				output_tokens := int(ot)

				cached_input_tokens := 0
				if details, ok := usage["prompt_token_details"].(map[string]any); ok {
					ct, ok := details["cached_tokens"].(float64)
					if !ok {
						slog.Warn("could not get cached tokens data from response usage")
					}
					cached_input_tokens = int(ct)
				} else {
					slog.Warn("could not get prompt token details from response usage")
				}

				if input_tokens == 0 && output_tokens == 0 {
					break
				}

				// track usage
				id, ok := body["id"].(string)
				if !ok {
					slog.Error("body does not contain an id")
					break
				}
				e := cloudevents.New()
				e.SetID(id)
				e.SetSource("envoy-ext-proc")
				e.SetType("tokens")
				e.SetSubject(customerId)
				e.SetTime(time.Now())
				err = e.SetData("application/json", map[string]string{
					"input_tokens":        strconv.Itoa(input_tokens),
					"output_tokens":       strconv.Itoa(output_tokens),
					"cached_input_tokens": strconv.Itoa(cached_input_tokens),
				})
				if err != nil {
					slog.Error("could not set cloud events data", "err", err)
					break
				}
				ingCtx, cancel := context.WithTimeout(ctx, openMeterTimeout)
				_, err := s.client.IngestEventWithResponse(ingCtx, e)
				cancel()
				if err != nil {
					slog.Error("could not send usage data to openmetes", "err", err)
				}
			}

		default:
			slog.Error("unknown Request type", "type", v)
		}
		if err := srv.Send(resp); err != nil {
			slog.Error("send error", "err", err)
		}
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
