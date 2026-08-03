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
	"syscall"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
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
	EventType        string
}

type UserData struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type CloudEventData struct {
	Method             string `json:"method"`
	Path               string `json:"request_path"`
	Duration           string `json:"duration"`
	ResponseCode       string `json:"response_code"`
	XForwardedFor      string `json:"x_forwarded_for"`
	XSub               string `json:"x_sub"`
	XUserName          string `json:"x_user_name"`
	XRequestID         string `json:"x_request_id"`
	GenAIRequestModel  string `json:"model"`
	GenAIResponseModel string `json:"response_model"`
	GenAIProviderName  string `json:"provider"`
	GenAIUsageInput    string `json:"input_tokens"`
	GenAIUsageOutput   string `json:"output_tokens"`
	GenAIUsageTotal    string `json:"total_tokens"`
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
		EventType:        os.Getenv("EVENT_TYPE"),
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

	data := CloudEventData{
		Method:             attrs["method"],
		Path:               attrs["request.path"],
		Duration:           attrs["duration"],
		ResponseCode:       attrs["response_code"],
		XForwardedFor:      attrs["x-forwarded-for"],
		XSub:               attrs["x-sub"],
		XUserName:          attrs["x-user-name"],
		XRequestID:         attrs["x-request-id"],
		GenAIRequestModel:  attrs["gen_ai.request.model"],
		GenAIResponseModel: attrs["gen_ai.response.model"],
		GenAIProviderName:  attrs["gen_ai.provider.name"],
		GenAIUsageInput:    attrs["gen_ai.usage.input_tokens"],
		GenAIUsageOutput:   attrs["gen_ai.usage.output_tokens"],
		GenAIUsageTotal:    attrs["gen_ai.usage.total_tokens"],
	}

	subject := attrs["x-sub"]

	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource("otlp-openmeter-bridge")
	event.SetType(s.config.EventType)
	event.SetSubject(subject)
	event.SetTime(time.Now())

	if err := event.SetData(cloudevents.ApplicationJSON, data); err != nil {
		return err
	}

	return s.sendWithRetry(ctx, event, data)
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

func (s *server) sendWithRetry(ctx context.Context, event cloudevents.Event, eventData CloudEventData) error {
	var err error
	for i := 0; i <= s.config.MaxRetries; i++ {
		if i > 0 {
			log.Printf("Retrying send (%d/%d) after error: %v", i, s.config.MaxRetries, err)
			time.Sleep(time.Duration(s.config.RetryWaitSeconds) * time.Second)
		}

		err = s.sendEvent(ctx, event, eventData)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to send event after %d retries: %w", s.config.MaxRetries, err)
}

func (s *server) sendEvent(ctx context.Context, event cloudevents.Event, eventData CloudEventData) error {
	// create consumer first
	userdata := UserData{
		Key:  event.Subject(),
		Name: eventData.XUserName,
	}
	b, err := json.Marshal(userdata)
	if err != nil {
		return err
	}
	log.Printf("creating customer at %s", fmt.Sprintf("%s/v3/openmeter/customers", s.config.RemoteAPIURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v3/openmeter/customers", s.config.RemoteAPIURL), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if s.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.BearerToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode != 409 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote API returned status %d: %s", resp.StatusCode, string(body))
	}

	b, err = json.Marshal(event)
	if err != nil {
		return err
	}
	log.Printf("creating event at %s", fmt.Sprintf("%s/v3/openmeter/events", s.config.RemoteAPIURL))
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v3/openmeter/events", s.config.RemoteAPIURL), bytes.NewReader(b))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ce-specversion", event.SpecVersion())
	req.Header.Set("ce-type", event.Type())
	req.Header.Set("ce-source", event.Source())
	req.Header.Set("ce-subject", event.Subject())
	req.Header.Set("ce-id", event.ID())

	if s.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.BearerToken)
	}

	resp, err = s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote API returned status %d: %s", resp.StatusCode, string(body))
	}

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
