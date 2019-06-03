package main

import (
	"github.com/docker/machine/libmachine/drivers/plugin"
	proxmoxve "github.com/lemupress/docker-machine-driver-proxmoxve/proxmoxve"
)

func main() {
	plugin.RegisterDriver(proxmoxve.NewDriver("default", ""))
}
