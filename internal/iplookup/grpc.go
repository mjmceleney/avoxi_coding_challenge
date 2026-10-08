package iplookup

import (
	"context"
	"errors"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	pb "iplookup/gen/iplookup/v1"
)

// grpcServer adapts Service to the generated IPLookupServiceServer interface.
type grpcServer struct {
	pb.UnimplementedIPLookupServiceServer
	svc *Service
}

// GRPCServer returns a *grpc.Server exposing the service:
//
//	iplookup.v1.IPLookupService/Check  {ip, allowed_countries} -> {country, allowed, reason}
//	grpc.health.v1.Health              SERVING while the server is up, matching GET /health
//
// Server reflection is enabled, so tools like grpcurl can discover the API without the
// .proto file.
//
// It mirrors the HTTP handler's behaviour, mapping errors to gRPC status codes
// (INVALID_ARGUMENT, INTERNAL). Serve it with grpc.Server.Serve and stop it with
// GracefulStop.
func (s *Service) GRPCServer(opts ...grpc.ServerOption) *grpc.Server {
	srv := grpc.NewServer(opts...)
	pb.RegisterIPLookupServiceServer(srv, &grpcServer{svc: s})
	healthpb.RegisterHealthServer(srv, health.NewServer())
	reflection.Register(srv)
	return srv
}

func (g *grpcServer) Check(_ context.Context, req *pb.CheckRequest) (*pb.CheckResponse, error) {
	res, err := g.svc.Check(req.GetIp(), req.GetAllowedCountries())
	switch {
	case errors.Is(err, ErrInvalidArgument):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case err != nil:
		log.Println("Error checking IP:", err)
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	return &pb.CheckResponse{Country: res.Country, Allowed: res.Allowed, Reason: pbReasons[res.Reason]}, nil
}

// pbReasons maps each Reason to its protobuf enum. The empty Reason (allowed) is absent,
// so it maps to the zero value, REASON_UNSPECIFIED.
var pbReasons = map[Reason]pb.Reason{
	ReasonCountryNotAllowed: pb.Reason_REASON_COUNTRY_NOT_ALLOWED,
	ReasonCountryUnknown:    pb.Reason_REASON_COUNTRY_UNKNOWN,
}
