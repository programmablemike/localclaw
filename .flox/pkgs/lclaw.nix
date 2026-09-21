# Nix expression build for lclaw. Run: flox build lclaw
# Output: ./result-lclaw/bin/lclaw
#
# Flox evaluates this against the same nixpkgs pin that provides the dev
# environment's Go, so there is one toolchain pin. Dependencies come from
# the committed vendor/ directory (vendorHash = null), so the build fetches
# nothing and fails loudly if a dependency is not vendored.
{ lib, buildGoModule }:

let
  version = lib.strings.removeSuffix "\n" (builtins.readFile ../../VERSION);
in
buildGoModule {
  pname = "lclaw";
  inherit version;

  # Only what the build needs. Keeps .flox, docs and git metadata out of
  # the store path so unrelated edits do not trigger rebuilds.
  src = lib.fileset.toSource {
    root = ../..;
    fileset = lib.fileset.unions [
      ../../go.mod
      ../../go.sum
      ../../VERSION
      ../../version.go
      ../../cmd
      ../../internal
      ../../vendor
    ];
  };

  vendorHash = null;
  env.CGO_ENABLED = 0;
  ldflags = [ "-s" "-w" ];

  # go test over every package, including the import-boundary test.
  doCheck = true;

  meta = {
    description = "Manage LocalClaw's Podman machines";
    homepage = "https://github.com/programmablemike/localclaw";
    license = lib.licenses.mit;
    mainProgram = "lclaw";
  };
}
