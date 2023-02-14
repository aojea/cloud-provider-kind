package loadbalancer

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"k8s.io/klog/v2"

	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	routeservice "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	runtimeservice "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	secretservice "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v3"
)

const (
	grpcKeepaliveTime        = 30 * time.Second
	grpcKeepaliveTimeout     = 5 * time.Second
	grpcKeepaliveMinTime     = 30 * time.Second
	grpcMaxConcurrentStreams = 1000000
)

type Server struct {
	cache  cache.Cache
	server xds.Server
}

func NewServer() *Server {
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)
	return &Server{
		cache: snapshotCache,
	}
}

func (s *Server) Run(ctx context.Context, port int) {
	s.server = xds.NewServer(ctx, s.cache, nil)
	// gRPC golang library sets a very small upper bound for the number gRPC/h2
	// streams over a single TCP connection. If a proxy multiplexes requests over
	// a single connection to the management server, then it might lead to
	// availability problems. Keepalive timeouts based on connection_keepalive parameter https://www.envoyproxy.io/docs/envoy/latest/configuration/overview/examples#dynamic
	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions,
		grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    grpcKeepaliveTime,
			Timeout: grpcKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             grpcKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
	)
	grpcServer := grpc.NewServer(grpcOptions...)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}

	// register services
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, s.server)
	endpointservice.RegisterEndpointDiscoveryServiceServer(grpcServer, s.server)
	clusterservice.RegisterClusterDiscoveryServiceServer(grpcServer, s.server)
	routeservice.RegisterRouteDiscoveryServiceServer(grpcServer, s.server)
	listenerservice.RegisterListenerDiscoveryServiceServer(grpcServer, s.server)
	secretservice.RegisterSecretDiscoveryServiceServer(grpcServer, s.server)
	runtimeservice.RegisterRuntimeDiscoveryServiceServer(grpcServer, s.server)

	go func() {
		log.Printf("management server listening on %d\n", port)
		if err = grpcServer.Serve(lis); err != nil {
			klog.Infof("xds server exit: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		lis.Close()
	}()
}
