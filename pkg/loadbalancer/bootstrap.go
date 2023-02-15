package loadbalancer

import (
	"bytes"
	"text/template"

	"github.com/pkg/errors"
)

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
