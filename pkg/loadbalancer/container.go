package loadbalancer

import (
	"io"
	"os"
	"os/exec"
	"strings"

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
