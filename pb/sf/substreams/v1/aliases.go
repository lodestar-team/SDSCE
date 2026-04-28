package v1

import pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"

// Buf managed codegen rewrites external go_package imports under this repo's pb prefix.
// Re-export the upstream Substreams package types locally so generated SDS stubs can
// reference the canonical Substreams protobuf messages without vendoring the full schema.
type Package = pbsubstreams.Package
