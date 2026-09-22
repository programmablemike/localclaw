package app_test

import (
	"github.com/programmablemike/localclaw/internal/adapters/flox"
	"github.com/programmablemike/localclaw/internal/adapters/keychain"
	"github.com/programmablemike/localclaw/internal/adapters/osfs"
	"github.com/programmablemike/localclaw/internal/adapters/podman"
	"github.com/programmablemike/localclaw/internal/adapters/toml"
	"github.com/programmablemike/localclaw/internal/app"
)

var (
	_ app.MachineRuntime     = (*podman.Client)(nil)
	_ app.EnvironmentManager = (*flox.Client)(nil)
	_ app.FileWriter         = osfs.System{}
	_ app.DirOpener          = osfs.System{}
	_ app.TopologyLoader     = toml.Loader{}
	_ app.Keychain           = (*keychain.Client)(nil)
	_ app.SecretTarget       = (*podman.Client)(nil)
)
