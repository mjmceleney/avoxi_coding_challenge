package iplookup

import (
	"context"
	"maps"
	"net"
	"slices"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "iplookup/gen/iplookup/v1"
)

func newTestGRPCConn(t *testing.T, s *Service) *grpc.ClientConn {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := s.GRPCServer()
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestGRPCCheck(t *testing.T) {
	client := pb.NewIPLookupServiceClient(newTestGRPCConn(t, newTestService(t)))

	tests := []struct {
		name     string
		req      *pb.CheckRequest
		wantCode codes.Code
		want     *pb.CheckResponse
	}{
		{"allowed", &pb.CheckRequest{Ip: "81.2.69.160", AllowedCountries: []string{"GB"}}, codes.OK, &pb.CheckResponse{Country: "GB", Allowed: true}},
		{"not allowed", &pb.CheckRequest{Ip: "81.2.69.160", AllowedCountries: []string{"JP"}}, codes.OK, &pb.CheckResponse{Country: "GB", Reason: pb.Reason_REASON_COUNTRY_NOT_ALLOWED}},
		{"unknown country", &pb.CheckRequest{Ip: "10.0.0.1", AllowedCountries: []string{"US"}}, codes.OK, &pb.CheckResponse{Reason: pb.Reason_REASON_COUNTRY_UNKNOWN}},
		{"invalid ip", &pb.CheckRequest{Ip: "nope", AllowedCountries: []string{"US"}}, codes.InvalidArgument, nil},
		{"no countries", &pb.CheckRequest{Ip: "81.2.69.160"}, codes.InvalidArgument, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.Check(context.Background(), tt.req)
			if code := status.Code(err); code != tt.wantCode {
				t.Fatalf("code = %v, want %v (err: %v)", code, tt.wantCode, err)
			}
			if tt.want != nil && !proto.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGRPCHealth(t *testing.T) {
	client := healthpb.NewHealthClient(newTestGRPCConn(t, newTestService(t)))
	resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", resp.Status)
	}
}

func TestGRPCReflection(t *testing.T) {
	services := newTestService(t).GRPCServer().GetServiceInfo()
	if _, ok := services["grpc.reflection.v1.ServerReflection"]; !ok {
		t.Errorf("reflection service not registered; got services %v", slices.Collect(maps.Keys(services)))
	}
}
