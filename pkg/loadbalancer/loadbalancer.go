package loadbalancer

import (
	"bytes"
	"text/template"

	"github.com/pkg/errors"
)

// Image defines the loadbalancer image:tag
const Image = "haproxy:lts-bullseye"

// ConfigPath defines the path to the config file in the image
const ConfigPath = "/usr/local/etc/haproxy/haproxy.cfg"

// ConfigData is supplied to the loadbalancer config template
type ConfigData struct {
	ServicePorts    []string
	HealthCheckPort int
	BackendServers  map[string]string
	IPv6            bool
}

// DefaultConfigTemplate is the loadbalancer config template
const DefaultConfigTemplate = `
global
  log /dev/log local0
  log /dev/log local1 notice
  daemon

resolvers docker
  nameserver dns 127.0.0.11:53

defaults
  log global
  mode tcp
  option dontlognull
  # TODO: tune these
  timeout connect 5000
  timeout client 50000
  timeout server 50000
  # allow to boot despite dns don't resolve backends
  default-server init-addr none

frontend service
{{ range $index, $port := .ServicePorts }}  bind *:{{ $port }}{{end}}
  default_backend nodes

backend nodes
  option httpchk GET /healthz
  {{ $hcport := .HealthCheckPort }}
{{range $server, $address := .BackendServers}}
  server {{ $server }} {{ $address }} check port {{ $hcport }} inter 5s  fall 3  rise 1
{{- end}}
`

// Config returns a kubeadm config generated from config data, in particular
// the kubernetes version
func Config(data *ConfigData) (config string, err error) {
	t, err := template.New("loadbalancer-config").Parse(DefaultConfigTemplate)
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
