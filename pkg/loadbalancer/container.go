package loadbalancer

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	kindexec "sigs.k8s.io/kind/pkg/exec"
)

// KIND CONSTANTS
const fixedNetworkName = "kind"
const clusterLabelKey = "io.x-k8s.kind.cluster"
const nodeRoleLabelKey = "io.x-k8s.kind.role"

func createContainer(name string, args []string) error {
	if err := exec.Command("docker", append([]string{"run", "--name", name}, args...)...).Run(); err != nil {
		return err
	}
	return nil
}

func deleteContainer(name string) error {
	if err := exec.Command("docker", []string{"rm", "-f", name}...).Run(); err != nil {
		return err
	}
	return nil
}

func restartContainer(name string) error {
	if err := exec.Command("docker", []string{"restart", name}...).Run(); err != nil {
		return err
	}
	return nil
}

func execContainer(name string, command []string, stdin io.Reader) (stdout io.Writer, stderr io.Writer, err error) {
	args := []string{"exec", "--privileged"}
	if stdin != nil {
		args = append(args, "-i")
	}
	args = append(args, name)
	args = append(args, command...)
	cmd := exec.Command("docker", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	return
}

func containerIPs(name string) (ipv4 string, ipv6 string, err error) {
	// retrieve the IP address of the node using docker inspect
	cmd := kindexec.Command("docker", "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}},{{.GlobalIPv6Address}}{{end}}",
		name, // ... against the "node" container
	)
	lines, err := kindexec.OutputLines(cmd)
	if err != nil {
		return "", "", errors.Wrap(err, "failed to get container details")
	}
	if len(lines) != 1 {
		return "", "", errors.Errorf("file should only be one line, got %d lines", len(lines))
	}
	ips := strings.Split(lines[0], ",")
	if len(ips) != 2 {
		return "", "", errors.Errorf("container addresses should have 2 values, got %d values", len(ips))
	}
	return ips[0], ips[1], nil
}

func networkGateways() (ipv4 string, ipv6 string, err error) {
	// retrieve the IP address of the docker bridge using docker inspect
	networkName := fixedNetworkName
	if n := os.Getenv("KIND_EXPERIMENTAL_DOCKER_NETWORK"); n != "" {
		networkName = n
	}

	cmd := kindexec.Command("docker", "network", "inspect", networkName,
		"-f", "{{range .IPAM.Config}}{{.Gateway}} {{end}}")
	lines, err := kindexec.OutputLines(cmd)
	if err != nil {
		return "", "", errors.Wrap(err, "failed to get gateway details")
	}
	if len(lines) != 1 {
		return "", "", errors.Errorf("file should only be one line, got %d lines", len(lines))
	}
	output := strings.TrimSpace(lines[0])
	ips := strings.Split(output, " ")
	if len(ips) != 2 {
		return "", "", errors.Errorf("network gateway should have 2 values, got %d values", len(ips))
	}
	return ips[0], ips[1], nil
}

// Image defines the loadbalancer image:tag
const Image = "envoyproxy/envoy:v1.24.2"

// ConfigPath defines the path to the config file in the image
const configPath = "/etc/envoy/envoy.yaml"

type configData struct {
	ControlPlaneAddress string
	ControlPlanePort    int
	LoadBalancerName    string
	ClusterName         string
}

const bootstrapTemplate = `
node:
  cluster: {{ .LoadBalancerName }}-cluster
  id: {{ .LoadBalancerName }}-id

dynamic_resources:
  ads_config:
    api_type: GRPC
    transport_api_version: V3
    grpc_services:
    - envoy_grpc:
        cluster_name: {{ .ClusterName }}
  cds_config:
    resource_api_version: V3
    ads: {}
  lds_config:
    resource_api_version: V3
    ads: {}

static_resources:
  clusters:
  - type: STRICT_DNS
    typed_extension_protocol_options:
      envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
        "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
        explicit_http_config:
          http2_protocol_options: {}
    name: {{ .ClusterName }}
    load_assignment:
      cluster_name: {{ .ClusterName }}
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: {{ .ControlPlaneAddress }}
                port_value: {{ .ControlPlanePort }}

admin:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 19000
`

// Config returns a kubeadm config generated from config data, in particular
// the kubernetes version
func config(data *configData) (config string, err error) {
	t, err := template.New("loadbalancer-config").Parse(bootstrapTemplate)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse config template")
	}
	// execute the template
	var buff bytes.Buffer
	err = t.Execute(&buff, data)
	if err != nil {
		return "", errors.Wrap(err, "error executing config template")
	}
	return buff.String(), nil
}
