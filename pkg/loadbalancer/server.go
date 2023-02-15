package loadbalancer

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kind/pkg/cluster/constants"

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
	port   int
}

func NewServer() *Server {
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)
	return &Server{
		cache: snapshotCache,
	}
}

func (s *Server) Run(ctx context.Context, port int) {
	s.server = xds.NewServer(ctx, s.cache, nil)
	s.port = port
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

func (s *Server) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	// report status
	name := loadBalancerName(clusterName, service)
	ipv4, ipv6, err := containerIPs(name)
	if err != nil {
		if strings.Contains(err.Error(), "failed to get container details") {
			return nil, false, nil
		}
		return nil, false, err
	}
	status := &v1.LoadBalancerStatus{}
	svcIPv4 := false
	svcIPv6 := false
	for _, family := range service.Spec.IPFamilies {
		if family == v1.IPv4Protocol {
			svcIPv4 = true
		}
		if family == v1.IPv6Protocol {
			svcIPv6 = true
		}
	}
	if ipv4 != "" && svcIPv4 {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ipv4})
	}
	if ipv6 != "" && svcIPv6 {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ipv4})
	}
	return status, true, nil
}

func (s *Server) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	return loadBalancerName(clusterName, service)
}

func (s *Server) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	err := createLoadBalancer(clusterName, service)
	if err != nil {
		return nil, err
	}
	// configure bootstrap
	gw4, _, err := networkGateways()
	if err != nil {
		return nil, err
	}
	name := loadBalancerName(clusterName, service)

	bootstrap, err := config(&configData{
		ControlPlaneAddress: gw4, // listen on the container gateway IP
		ControlPlanePort:    s.port,
		LoadBalancerName:    name,
		ClusterName:         clusterName,
	})
	if err != nil {
		return nil, err
	}

	_, _, err = execContainer(name, []string{"cp", "/dev/stdin", configPath}, strings.NewReader(bootstrap))
	if err != nil {
		return nil, fmt.Errorf("failed to apply bootstrap config: %w", err)
	}

	err = restartContainer(name)
	if err != nil {
		return nil, err
	}

	// get loadbalancer Status
	ipv4, ipv6, err := containerIPs(name)
	if err != nil {
		return nil, err
	}
	status := &v1.LoadBalancerStatus{}
	svcIPv4 := false
	svcIPv6 := false
	for _, family := range service.Spec.IPFamilies {
		if family == v1.IPv4Protocol {
			svcIPv4 = true
		}
		if family == v1.IPv6Protocol {
			svcIPv6 = true
		}
	}
	if ipv4 != "" && svcIPv4 {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ipv4})
	}
	if ipv6 != "" && svcIPv6 {
		status.Ingress = append(status.Ingress, v1.LoadBalancerIngress{IP: ipv4})
	}
	return status, nil
}

func (s *Server) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return fmt.Errorf("NOT IMPLEMENTED")
}

func (s *Server) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	return deleteContainer(loadBalancerName(clusterName, service))
}

// loadbalancer name = cluster-name + service.namespace + service.name
func loadBalancerName(clusterName string, service *v1.Service) string {
	return clusterName + "-" + service.Namespace + "-" + service.Name
}

// createLoadBalancer create a docker container with a loadbalancer
func createLoadBalancer(clusterName string, service *v1.Service) error {
	name := loadBalancerName(clusterName, service)

	networkName := fixedNetworkName
	if n := os.Getenv("KIND_EXPERIMENTAL_DOCKER_NETWORK"); n != "" {
		networkName = n
	}

	args := []string{
		"--detach", // run the container detached
		"--tty",    // allocate a tty for entrypoint logs
		// label the node with the cluster ID
		"--label", fmt.Sprintf("%s=%s", clusterLabelKey, clusterName),
		"--label", fmt.Sprintf("%s=%s", nodeRoleLabelKey, constants.ExternalLoadBalancerNodeRoleValue),
		// user a user defined docker network so we get embedded DNS
		"--net", networkName,
		"--init=false",
		"--hostname", name, // make hostname match container name
		// label the node with the role ID
		// running containers in a container requires privileged
		// NOTE: we could try to replicate this with --cap-add, and use less
		// privileges, but this flag also changes some mounts that are necessary
		// including some ones docker would otherwise do by default.
		// for now this is what we want. in the future we may revisit this.
		"--privileged",
		"--image", Image,
		"--restart=on-failure:1", // to allow to change the configuration
	}

	args = append(args, "--sysctl=net.ipv6.conf.all.disable_ipv6=0", "--sysctl=net.ipv6.conf.all.forwarding=1")

	err := createContainer(name, args)
	if err != nil {
		return err
	}

	return nil
}
