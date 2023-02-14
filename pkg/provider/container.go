package provider

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	kindexec "sigs.k8s.io/kind/pkg/exec"
)

func createContainer(name string, args []string) error {
	if err := exec.Command("docker", append([]string{"run", "--name", name}, args...)...).Run(); err != nil {
		return err
	}
	return nil
}

func deleteContainer(name string) error {
	if err := exec.Command("docker", []string{"rm", "-f", "--name", name}...).Run(); err != nil {
		return err
	}
	return nil
}

func execContainer(name string, command string) (stdout io.Writer, stderr io.Writer, err error) {
	args := []string{"exec", "-i", "--privileged", "--name", name}
	cmd := exec.Command("docker", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	return
}

func containerIPs(name string) (ipv4 string, ipv6 string, err error) {
	// retrieve the IP address of the node using podman inspect
	cmd := kindexec.Command("podman", "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}},{{.GlobalIPv6Address}}{{end}}",
		name, // ... against the "node" container
	)
	lines, err := kindexec.OutputLines(cmd)
	if err != nil {
		return "", "", fmt.Errorf("failed to get container details: %w", err)
	}
	if len(lines) != 1 {
		return "", "", fmt.Errorf("file should only be one line, got %d lines", len(lines))
	}
	ips := strings.Split(lines[0], ",")
	if len(ips) != 2 {
		return "", "", fmt.Errorf("container addresses should have 2 values, got %d values", len(ips))
	}
	return ips[0], ips[1], nil
}
