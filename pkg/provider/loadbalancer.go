package provider

import (
	"context"
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kind/pkg/cluster/constants"
)

var _ cloudprovider.LoadBalancer = &cloud{}

// GetLoadBalancer returns whether the specified load balancer exists, and if so, what its status is.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *cloud) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (status *v1.LoadBalancerStatus, exists bool, err error) {
	klog.V(2).Infof("Get LoadBalancer cluster: %s service: %s", clusterName, service.Name)
	return nil, false, nil
}

// GetLoadBalancerName returns the name of the load balancer.
func (c *cloud) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	klog.V(2).Infof("Get LoadBalancerNmae cluster: %s service: %s", clusterName, service.Name)
	return loadBalancerName(clusterName, service)
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
func (c *cloud) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	klog.V(2).Infof("Ensure LoadBalancer cluster: %s service: %s nodes: %v", clusterName, service.Name, nodes)
	err := createLoadBalancer(clusterName, service)
	if err != nil {
		return nil, err
	}

	// configure loadbalancer

	// report status
	name := loadBalancerName(clusterName, service)
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

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (c *cloud) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	klog.V(2).Infof("Update LoadBalancer cluster: %s service: %s nodes: %v", clusterName, service.Name, nodes)
	return fmt.Errorf("NOT IMPLEMENTED")
}

// EnsureLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
func (c *cloud) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	klog.V(2).Infof("Ensure LoadBalancer deleted cluster: %s service: %s", clusterName, service.Name)
	return deleteContainer(loadBalancerName(clusterName, service))
}

const loadbalancerImage = "envoyproxy/envoy:v1.24.2"

// KIND CONSTANTS
const fixedNetworkName = "kind"
const clusterLabelKey = "io.x-k8s.kind.cluster"
const nodeRoleLabelKey = "io.x-k8s.kind.role"

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
		"--image", loadbalancerImage,
	}

	args = append(args, "--sysctl=net.ipv6.conf.all.disable_ipv6=0", "--sysctl=net.ipv6.conf.all.forwarding=1")

	return createContainer(name, args)
}

// updateLoadBalancer updates the specified container loadbalancer
func updateLoadBalancer(clusterName string, service *v1.Service, nodes []*v1.Node) error {
	name := loadBalancerName(clusterName, service)
	command := `ls`
	out, stderr, err := execContainer(name, command)
	klog.Info("Executed commands %s, out: %s err: %s", out, stderr)
	return err
}
