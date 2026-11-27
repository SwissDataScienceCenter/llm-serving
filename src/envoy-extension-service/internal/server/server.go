// Package server wires and registers all gRPC services.
package server

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/sdsc-vllm/envoy-extension-service/internal/config"
	"github.com/sdsc-vllm/envoy-extension-service/internal/service"
)

// New returns a gRPC server with all extension services registered.
func New(cfg *config.Config) (*grpc.Server, error) {
	gs := grpc.NewServer()

	// gRPC health service, used by load balancers / readiness probes.
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, hs)

	meteringSvc, err := service.NewExternalProcessorMeteringService(cfg)
	if err != nil {
		return nil, err
	}
	envoy_service_proc_v3.RegisterExternalProcessorServer(gs, meteringSvc)

	return gs, nil
}
