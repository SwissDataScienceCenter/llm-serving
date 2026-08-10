package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsrc "go.opentelemetry.io/proto/otlp/logs/v1"
)

type Config struct {
	RemoteAPIURL     string
	BearerToken      string
	GRPCListenAddr   string
	MaxRetries       int
	RetryWaitSeconds int
	MetricCode       string
	CustomerID       string
}

// LagoEvent maps to the Lago Event schema for ingestion
type LagoEvent struct {
	TransactionID          string                 `json:"transaction_id"`
	ExternalSubscriptionID string                 `json:"external_subscription_id"`
	Code                   string                 `json:"code"`
	Timestamp              int64                  `json:"timestamp"`
	Properties             map[string]interface{} `json:"properties,omitempty"`
}

// LagoIngestRequest maps to the Lago Event ingestion request
type LagoIngestRequest struct {
	Event LagoEvent `json:"event"`
}

type server struct {
	logspb.UnimplementedLogsServiceServer
	config *Config
	client *http.Client
}

func loadConfig() *Config {
	return &Config{
		RemoteAPIURL:     os.Getenv("REMOTE_API_URL"),
		BearerToken:      os.Getenv("BEARER_TOKEN"),
		GRPCListenAddr:   os.Getenv("GRPC_LISTEN_ADDR"),
		MaxRetries:       3,
		RetryWaitSeconds: 2,
		MetricCode:       os.Getenv("METRIC_CODE"),
		CustomerID:       os.Getenv("CUSTOMER_ID"),
	}
}

func (s *server) Export(ctx context.Context, req *logspb.ExportLogsServiceRequest) (*logspb.ExportLogsServiceResponse, error) {
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				if err := s.processLogRecord(ctx, lr); err != nil {
					log.Printf("Error processing log record: %v", err)
				}
			}
		}
	}
	return &logspb.ExportLogsServiceResponse{}, nil
}

func (s *server) processLogRecord(ctx context.Context, lr *logsrc.LogRecord) error {
	attrs := make(map[string]string)
	for _, kv := range lr.Attributes {
		attrs[kv.Key] = AnyValueToString(kv.Value)
	}

	// Extract fields from OTLP attributes
	method := attrs["method"]
	path := attrs["request.path"]
	duration := attrs["duration"]
	responseCode := attrs["response_code"]

	if code, err := strconv.Atoi(responseCode); err != nil || code > 300 {
		// only log actual requests
		return nil
	}

	xForwardedFor := attrs["x-forwarded-for"]
	subject := attrs["x-sub"]
	xUserName := attrs["x-user-name"]
	customerId := s.config.CustomerID
	if customerId == "" {
		log.Printf("No CUSTOMER_ID configured, skipping event")
		return nil
	}
	xRequestID := attrs["x-request-id"]
	genAIRequestModel := attrs["gen_ai.request.model"]
	genAIResponseModel := attrs["gen_ai.response.model"]
	genAIProviderName := attrs["gen_ai.provider.name"]
	genAIUsageInput := attrs["gen_ai.usage.input_tokens"]
	genAIUsageOutput := attrs["gen_ai.usage.output_tokens"]
	genAIUsageTotal := attrs["gen_ai.usage.total_tokens"]

	// Determine metric code from env or derive from context
	metricCode := s.config.MetricCode
	if metricCode == "" {
		metricCode = "genai.tokens"
	}

	// Build properties from OTLP attributes
	properties := make(map[string]interface{})
	if method != "" {
		properties["method"] = method
	}
	if path != "" {
		properties["request_path"] = path
	}
	if duration != "" {
		properties["duration"] = duration
	}
	if responseCode != "" {
		properties["response_code"] = responseCode
	}
	if xForwardedFor != "" {
		properties["x_forwarded_for"] = xForwardedFor
	}
	if subject != "" {
		properties["x-sub"] = subject
	}
	if xUserName != "" {
		properties["x-user-name"] = xUserName
	}
	if xRequestID != "" {
		properties["x_request_id"] = xRequestID
	}
	if genAIRequestModel != "" {
		properties["gen_ai.request.model"] = genAIRequestModel
	}
	if genAIResponseModel != "" {
		properties["gen_ai.response.model"] = genAIResponseModel
	}
	if genAIProviderName != "" {
		properties["gen_ai.provider.name"] = genAIProviderName
	}
	if genAIUsageInput != "" {
		properties["gen_ai.usage.input_tokens"] = genAIUsageInput
	}
	if genAIUsageOutput != "" {
		properties["gen_ai.usage.output_tokens"] = genAIUsageOutput
	}
	if genAIUsageTotal != "" {
		properties["gen_ai.usage.total_tokens"] = genAIUsageTotal
	}

	// Generate structured transaction ID: {type}_{date}_{customer}_{category}_{request_id}
	// Example: genai_20240314_cust42_gpt4_x-request-id-123
	dateStr := time.Now().UTC().Format("20060102")
	modelSlug := "unknown"
	if genAIRequestModel != "" {
		modelSlug = genAIRequestModel
	} else if genAIResponseModel != "" {
		modelSlug = genAIResponseModel
	}
	txnID := fmt.Sprintf("genai_%s_%s_%s_%s", dateStr, customerId, modelSlug, xRequestID)

	// Use the log record's timestamp if available, otherwise use current time
	var timestamp int64
	if lr.GetTimeUnixNano() > 0 {
		timestamp = int64(lr.GetTimeUnixNano() / 1000000000) // convert nanoseconds to seconds
	} else {
		timestamp = time.Now().UTC().Unix()
	}

	// Build the Lago event
	event := LagoEvent{
		TransactionID:          txnID,
		ExternalSubscriptionID: customerId,
		Code:                   metricCode,
		Timestamp:              timestamp,
		Properties:             properties,
	}

	return s.sendWithRetry(ctx, event)
}

func AnyValueToString(av *commonv1.AnyValue) string {
	switch av.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return av.GetStringValue()
	case *commonv1.AnyValue_IntValue:
		return fmt.Sprintf("%d", av.GetIntValue())
	case *commonv1.AnyValue_DoubleValue:
		return fmt.Sprintf("%f", av.GetDoubleValue())
	case *commonv1.AnyValue_BoolValue:
		return fmt.Sprintf("%t", av.GetBoolValue())
	case *commonv1.AnyValue_ArrayValue:
		// Recursively convert array elements
		return fmt.Sprintf("%v", av.GetArrayValue())
	case *commonv1.AnyValue_KvlistValue:
		// Recursively convert map values
		m := make(map[string]interface{})
		for _, kv := range av.GetKvlistValue().GetValues() {
			m[kv.Key] = AnyValueToString(kv.Value)
		}
		return fmt.Sprintf("%v", m)
	default:
		return ""
	}
}

func (s *server) sendWithRetry(ctx context.Context, event LagoEvent) error {
	var err error
	for i := 0; i <= s.config.MaxRetries; i++ {
		if i > 0 {
			log.Printf("Retrying send (%d/%d) after error: %v", i, s.config.MaxRetries, err)
			time.Sleep(time.Duration(s.config.RetryWaitSeconds) * time.Second)
		}

		err = s.sendEvent(ctx, event)
		if err == nil {
			return nil
		}

		// Check if it's a 429 rate limit error - use backoff
		if strings.Contains(err.Error(), "status 429") {
			backoff := time.Duration(i+1) * time.Duration(s.config.RetryWaitSeconds) * time.Second
			log.Printf("Rate limited, backing off for %v before retry", backoff)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("failed to send event after %d retries: %w", s.config.MaxRetries, err)
}

func (s *server) sendEvent(ctx context.Context, event LagoEvent) error {
	reqBody := LagoIngestRequest{
		Event: event,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/events", s.config.RemoteAPIURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if s.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.BearerToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("remote API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("Successfully ingested event: transaction_id=%s external_subscription_id=%s", event.TransactionID, event.ExternalSubscriptionID)
	return nil
}

func main() {
	cfg := loadConfig()
	if cfg.RemoteAPIURL == "" || cfg.GRPCListenAddr == "" {
		log.Fatal("REMOTE_API_URL and GRPC_LISTEN_ADDR must be set")
	}

	lis, err := net.Listen("tcp", cfg.GRPCListenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	srv := &server{
		config: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	logspb.RegisterLogsServiceServer(s, srv)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting gRPC server on %s", cfg.GRPCListenAddr)
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down gRPC server...")
	s.GracefulStop()
	log.Println("Server stopped")
}
