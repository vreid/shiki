package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	image_processor_v1alpha1 "github.com/vreid/shiki/libs/go/proto/image_processor/v1alpha1"
)

var (
	port = flag.Int("port", 50051, "")
)

type imageProcessorServer struct {
	image_processor_v1alpha1.UnimplementedImageProcessorServiceServer
}

func newServer() *imageProcessorServer {
	server := &imageProcessorServer{}

	return server
}

func main() {
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	lc := net.ListenConfig{}

	listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	opts := []grpc.ServerOption{}

	grpcServer := grpc.NewServer(opts...)
	image_processor_v1alpha1.RegisterImageProcessorServiceServer(grpcServer, newServer())

	go func() {
		<-ctx.Done()
		log.Println("shutting down gracefully...")

		grpcServer.GracefulStop()
	}()

	log.Printf("server listening on port %d", *port)

	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
