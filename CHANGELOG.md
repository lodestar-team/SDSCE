# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Added

- Add `sds.GRT` type for GRT token amounts with 18 decimal precision
  - Backed by `holiman/uint256` for efficient arithmetic
  - Supports parsing "X GRT" strings and plain decimal numbers
  - Implements `encoding.TextMarshaler/TextUnmarshaler`, `json.Marshaler/Unmarshaler`, and `yaml.Marshaler/Unmarshaler`
- Add `sds://` scheme plugins for firehose-core integration (`provider/plugin` package)
  - `plugin.RegisterAuth()` - registers `sds://` with dauth for RAV-based authentication
  - `plugin.RegisterSession()` - registers `sds://` with dsession for worker pool management
  - `plugin.RegisterMetering()` - registers `sds://` with dmetering for usage tracking
  - `plugin.Register()` - convenience function to register all three plugins at once
- Plugins are gRPC/Connect clients that connect to the provider gateway
- All business logic (service provider, escrow, quotas) is configured on the gateway, not the plugin
- Plugin configuration is minimal: `sds://host:port?plaintext=true&network=my-network`

### Changed

- Refactor `PricingConfig` to use `sds.GRT` type instead of `*big.Int` for prices
  - Pricing YAML now accepts "X GRT" format (e.g., `price_per_block: "0.000001 GRT"`) or plain decimals
