package loadbalancer

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"
	"sigs.k8s.io/kind/pkg/cluster/constants"
)

type Server struct {
	cache map[string]bool
}

var _ cloudprovider.LoadBalancer = &Server{}

func NewServer() *Server {
	return &Server{
		cache: map[string]bool{},
	}
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
	name := loadBalancerName(clusterName, service)
	if !containerExist(name) {
		klog.V(2).Infof("creating container for loadbalancer")
		err := createLoadBalancer(clusterName, service)
		if err != nil {
			return nil, err
		}
	}

	// update loadbalancer
	klog.V(2).Infof("updating loadbalancer")
	err := s.UpdateLoadBalancer(ctx, clusterName, service, nodes)
	if err != nil {
		return nil, err
	}

	// get loadbalancer Status
	klog.V(2).Infof("get loadbalancer status")
	status, ok, err := s.GetLoadBalancer(ctx, clusterName, service)
	if !ok {
		return nil, fmt.Errorf("loadbalancer %s not found", name)
	}
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (s *Server) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	name := loadBalancerName(clusterName, service)
	if service == nil {
		return nil
	}
	config := &ConfigData{
		HealthCheckPort: 10256, // kube-proxy default port
		BackendServers:  map[string]string{},
		ServicePorts:    []string{},
	}
	if service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal {
		config.HealthCheckPort = int(service.Spec.HealthCheckNodePort)
	}

	backendsV4 := map[string]string{}
	backendsV6 := map[string]string{}
	for _, n := range nodes {
		for _, addr := range n.Status.Addresses {
			if addr.Type == v1.NodeInternalIP {
				if netutils.IsIPv4String(addr.Address) {
					backendsV4[n.Name] = addr.Address
				} else if netutils.IsIPv6String(addr.Address) {
					backendsV6[n.Name] = addr.Address
				}
			}
		}
	}

	// TODO: support UDP and IPv6
	for _, port := range service.Spec.Ports {
		if port.Protocol != v1.ProtocolTCP {
			continue
		}
		portString := strconv.Itoa(int(port.Port))
		config.ServicePorts = append(config.ServicePorts, portString)
		for server, address := range backendsV4 {
			config.BackendServers[server] = net.JoinHostPort(address, portString)
		}
	}

	// create loadbalancer config data
	loadbalancerConfig, err := Config(config)
	if err != nil {
		return errors.Wrap(err, "failed to generate loadbalancer config data")
	}

	klog.V(2).Infof("updating loadbalancer with config %s", loadbalancerConfig)
	var stdout, stderr bytes.Buffer
	err = execContainer(name, []string{"cp", "/dev/stdin", ConfigPath}, strings.NewReader(loadbalancerConfig), &stdout, &stderr)
	if err != nil {
		return err
	}

	klog.V(2).Infof("restartin loadbalancer")
	err = execContainer(name, []string{"kill", "-s", "HUP", "1"}, nil, &stdout, &stderr)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	return deleteContainer(loadBalancerName(clusterName, service))
}

// loadbalancer name = cluster-name + service.namespace + service.name
func loadBalancerName(clusterName string, service *v1.Service) string {
	return "kindlb-" + clusterName + "-" + service.Namespace + "-" + service.Name
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
		"--restart=on-failure:1",                    // to allow to change the configuration
		"--sysctl=net.ipv4.ip_forward=1",            // allow ip forwarding
		"--sysctl=net.ipv6.conf.all.disable_ipv6=0", // enable IPv6
		"--sysctl=net.ipv6.conf.all.forwarding=1",   // allow ipv6 forwarding
		"--sysctl=net.ipv4.conf.all.rp_filter=0",    // disable rp filter
		Image,
	}

	err := createContainer(name, args)
	if err != nil {
		return fmt.Errorf("failed to create continers %s %v: %w", name, args, err)
	}

	return nil
}
