package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	image_processor_v1alpha1 "github.com/vreid/shiki/libs/go/proto/image_processor/v1alpha1"
)

var (
	port      = flag.Int("port", 50051, "")
	dataDir   = flag.String("data-dir", "/data", "")
	scriptDir = flag.String("script-dir", "/app/tools", "")
)

type imageProcessorServer struct {
	image_processor_v1alpha1.UnimplementedImageProcessorServiceServer

	dataDir   string
	scriptDir string
}

func newServer(dataDir, scriptDir string) *imageProcessorServer {
	server := &imageProcessorServer{
		dataDir:   dataDir,
		scriptDir: scriptDir,
	}

	return server
}

func (s *imageProcessorServer) UploadImage(ctx context.Context,
	req *image_processor_v1alpha1.UploadImageRequest) (*image_processor_v1alpha1.UploadImageResponse, error) {
	imageData := req.GetImageData()

	if len(imageData) == 0 {
		//nolint:wrapcheck
		return nil, status.Error(codes.InvalidArgument, "image_data cannot be empty")
	}

	tempFile, err := os.CreateTemp("", "upload-*.img")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create temp file: %v", err)
	}

	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
	}()

	_, err = tempFile.Write(imageData)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to write image data: %v", err)
	}

	err = tempFile.Close()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to close temp file: %v", err)
	}

	_, err = os.Stat(s.dataDir)
	if os.IsNotExist(err) {
		return nil, status.Errorf(codes.FailedPrecondition, "data directory does not exist: %s", s.dataDir)
	}

	scriptPath := filepath.Join(s.scriptDir, "process-image.sh")

	//nolint:gosec
	cmd := exec.CommandContext(ctx, scriptPath, tempFile.Name())
	cmd.Dir = s.dataDir

	output, err := cmd.Output()
	if err != nil {
		log.Printf("process-image.sh failed: %v", err)

		return nil, status.Errorf(codes.Internal, "failed to process image: %v", err)
	}

	uuid := strings.TrimSpace(string(output))

	if uuid == "" {
		//nolint:wrapcheck
		return nil, status.Error(codes.Internal, "script returned empty UUID")
	}

	return &image_processor_v1alpha1.UploadImageResponse{
		Uuid: uuid,
	}, nil
}

func main() {
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	lc := net.ListenConfig{}

	listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		//nolint:gocritic
		log.Fatalf("failed to listen: %v", err)
	}

	opts := []grpc.ServerOption{}

	grpcServer := grpc.NewServer(opts...)
	image_processor_v1alpha1.RegisterImageProcessorServiceServer(grpcServer,
		newServer(*dataDir, *scriptDir))

	reflection.Register(grpcServer)

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
