package loadbalancer

import (
	"bytes"
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// dsrImage defines the loadbalancer image:tag
const dsrImage = "aojea/nft:v0.0.1"

// nft add rule netdev t c tcp dport 80 ether saddr set aa:bb:cc:dd:ff:ee ether daddr set jhash ip saddr . tcp sport mod 2 map { 0 : xx:xx:xx:xx:xx:xx, 1: yy:yy:yy:yy:yy:yy } fwd to eth0
func dsrUpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	name := loadBalancerName(clusterName, service)
	if service == nil {
		return nil
	}

	healthCheckPort := 10256 // kube-proxy default port
	if service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal {
		healthCheckPort = int(service.Spec.HealthCheckNodePort)
	}
	klog.Info("healtcheck port not implemented yet: %d", healthCheckPort)

	// destination mac
	backends := []string{}
	for _, n := range nodes {
		mac, err := containerMac(n.Name)
		if err != nil {
			return fmt.Errorf("could not get max address for node %s: %w", n.Name, err)
		}
		backends = append(backends, mac)
	}

	// destination port for filtering incoming traffic
	frontends := []int{}
	for _, port := range service.Spec.Ports {
		frontends = append(frontends, int(port.Port))
	}

	// create loadbalancer config data
	var config, stdout, stderr bytes.Buffer

	klog.V(2).Infof("updating loadbalancer with config %s", config)
	err := execContainer(name, []string{"cp", "/dev/stdin", proxyConfigPath}, &config, &stdout, &stderr)
	if err != nil {
		return fmt.Errorf("unexpected error adding loadbalancer rules stdout: %s stderr: %s error: %w", stdout.String(), stderr.String(), err)
	}
	return nil
}
