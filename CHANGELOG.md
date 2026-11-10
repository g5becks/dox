# Changelog

## [Unreleased]

### Changed

- Building from source requires Go 1.26 or newer; development uses Go 1.27.3. Upgrade older Go installations before building. Go 1.26 retains compatibility with the oldest supported Go line while enabling the standard library modernizations.

### Maintenance

- Applied Go modernizers and simplified sorted map iteration and test contexts without changing CLI behavior.
