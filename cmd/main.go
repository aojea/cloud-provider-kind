package main

import (
	"github.com/aojea/cloud-provider-kind/cmd/app"

	_ "github.com/aojea/cloud-provider-kind/pkg/provider" // initialize cloud provider
)

func main() {
	app.Main()
}
