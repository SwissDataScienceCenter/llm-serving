// Command envoy-extension-service runs the Envoy extension gRPC services.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sdsc-vllm/envoy-extension-service/internal/config"
	"github.com/sdsc-vllm/envoy-extension-service/internal/server"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to the config file (optional)")
		listenAddr = flag.String("listen", "", "gRPC listen address (overrides config)")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	addr := *listenAddr
	if addr == "" {
		addr = net.JoinHostPort(cfg.GRPC.Host, cfg.GRPC.Port)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listening on %s: %v", addr, err)
	}

	gs, err := server.New(cfg)
	if err != nil {
		log.Fatalf("could not create server: %v", err)
	}

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		gs.GracefulStop()
	}()

	log.Printf("serving gRPC on %s", addr)
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("serving gRPC: %v", err)
	}
}
