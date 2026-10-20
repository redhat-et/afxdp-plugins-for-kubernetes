# Vendored bpfman gRPC stubs

The generated protobuf/gRPC code in this directory (`bpfman.pb.go`,
`bpfman_grpc.pb.go`) is copied verbatim from the
[bpfman](https://github.com/bpfman/bpfman) repository's
`clients/gobpfman/v1` package (Apache-2.0). It is vendored here so that the
device plugin builds against the bpfman gRPC API without depending on
unreleased bpfman Go module versions.

Source: `github.com/bpfman/bpfman`, `clients/gobpfman/v1`, proto file
`proto/bpfman.proto` (package `bpfman.v1`, service `Bpfman`).

## Updating

When a bpfman release publishes a compatible `clients/gobpfman/v1`, replace
this directory with the released module (add
`github.com/bpfman/bpfman/clients/gobpfman/v1` as a dependency and change the
import in `internal/bpfman/client.go`), or refresh the copied files from a
newer checkout:

```sh
cp <bpfman-checkout>/clients/gobpfman/v1/*.go .
```

Then run `go test ./internal/bpfman/...` - the client tests exercise the
full surface used by the device plugin against an in-process fake server.

## Release checklist (bpfman API sync)

When cutting a release of this project, or after upgrading the bpfman daemon
on clusters:

1. Note the bpfman version deployed on nodes and compare with the source
   revision these stubs were copied from (see git history for this directory).
2. If bpfman's `proto/bpfman.proto` or generated Go stubs changed, refresh
   `internal/bpfman/v1` from that bpfman tag before tagging a release here.
3. Run `go test ./internal/bpfman/...` and bpfman-mode e2e if available.
4. Document the supported bpfman version range in the release notes when the
   stubs are updated.
