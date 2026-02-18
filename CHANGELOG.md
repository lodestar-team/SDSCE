# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Added

- Add `sds://` scheme plugins for firehose-core integration (`provider/plugin` package)
  - `plugin.RegisterAuth()` - registers `sds://` with dauth for RAV-based authentication
  - `plugin.RegisterSession()` - registers `sds://` with dsession for worker pool management
  - `plugin.RegisterMetering()` - registers `sds://` with dmetering for usage tracking
  - `plugin.Register()` - convenience function to register all three plugins at once
- Plugins are gRPC/Connect clients that connect to the provider sidecar
- All business logic (service provider, escrow, quotas) is configured on the sidecar, not the plugin
- Plugin configuration is minimal: `sds://host:port?plaintext=true&network=my-network`
