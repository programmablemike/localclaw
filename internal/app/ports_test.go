package app_test

import (
	"github.com/programmablemike/localclaw/internal/adapters/flox"
	"github.com/programmablemike/localclaw/internal/adapters/podman"
	"github.com/programmablemike/localclaw/internal/app"
)

var (
	_ app.MachineRuntime     = (*podman.Client)(nil)
	_ app.EnvironmentManager = (*flox.Client)(nil)
)
