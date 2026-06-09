## [1.1.4](https://github.com/martynvdijke/synapse/compare/v1.1.3...v1.1.4) (2026-06-09)

## [1.1.3](https://github.com/martynvdijke/synapse/compare/v1.1.2...v1.1.3) (2026-06-09)

## [1.1.2](https://github.com/martynvdijke/synapse/compare/v1.1.1...v1.1.2) (2026-06-09)


### Bug Fixes

* **deps:** update all non-major dependencies to v0.53.0 ([#10](https://github.com/martynvdijke/synapse/issues/10)) ([813a69c](https://github.com/martynvdijke/synapse/commit/813a69cc2d53c91790175f5cb9826d6cc1aa7a30))

## [1.1.1](https://github.com/martynvdijke/synapse/compare/v1.1.0...v1.1.1) (2026-06-08)

# [1.1.0](https://github.com/martynvdijke/synapse/compare/v1.0.4...v1.1.0) (2026-06-07)


### Bug Fixes

* add actions:read permission to release workflow ci job for reusable workflow ([8d50dd4](https://github.com/martynvdijke/synapse/commit/8d50dd49a82e29f3361d1570e9146da31b310645))
* invalid timezone UTC+1, use Europe/Amsterdam instead ([73cb8df](https://github.com/martynvdijke/synapse/commit/73cb8dfd667f61d15f05739c1bc07e3f2ffb9f1a))
* remove duplicate otel job from release.yaml (ci.yaml already exports traces when called as reusable workflow) ([706a9fb](https://github.com/martynvdijke/synapse/commit/706a9fb769798664a88daf4e5ac4846583e7a3dd))
* remove stalePr from renovate.json (no longer valid in Renovate v37) ([8cf6e3f](https://github.com/martynvdijke/synapse/commit/8cf6e3f7eed7089f56d11ccf57fb67269990f95c))
* remove stalePrAge from renovate.json (removed in Renovate v37) ([9a604d5](https://github.com/martynvdijke/synapse/commit/9a604d56a7ddcba29dc63de1e3b4c15f613803ec))
* resolve go.mod conflicts after rebase onto main, fix telemetry test signature ([149888e](https://github.com/martynvdijke/synapse/commit/149888e2c0996f6cc447cf13f23e55009ba9295c))
* restore concurrency and permissions on ci job call in release.yaml ([c104f6e](https://github.com/martynvdijke/synapse/commit/c104f6ed44f0622c49d96bc3186472a41e42dae8))
* restore release job to release.yaml, startup_failure was caused by missing permissions on reusable workflow call ([dd496ec](https://github.com/martynvdijke/synapse/commit/dd496ecac7a88a660dea58bc2afe799682626e14))
* revert otel-cicd-action inputs to use githubToken (v4 doesn't accept otlpAuthorization/otelToken) ([ba9dfce](https://github.com/martynvdijke/synapse/commit/ba9dfcece00d54b381ad1b9df6164a31172bebce))
* simplify release.yaml to minimum to isolate startup_failure ([1ea4e43](https://github.com/martynvdijke/synapse/commit/1ea4e435d40c9bff16266ddb24f520863dca2192))
* **ui:** add for/id to form labels, replace inline onclick handlers with addEventListener ([600b7a6](https://github.com/martynvdijke/synapse/commit/600b7a6fe25d67e40d42db0090b73cac39ed1556))


### Features

* add Authelia integration — config sync, alerts, temp access, ban management ([7674489](https://github.com/martynvdijke/synapse/commit/76744896d33a504aeb2b385351868fbc08f1fada))
* add OTel endpoint admin configuration with DB-backed settings ([4da63d9](https://github.com/martynvdijke/synapse/commit/4da63d9d1fdca5b14883eb0a2a497cbaf6acf2ec))
* add otlpAuthorization input for Bearer auth ([f59701d](https://github.com/martynvdijke/synapse/commit/f59701da54f6ce3e53d82635814b54959ffbce11))
* add socketio client for kuma uptime monitoring ([95677d8](https://github.com/martynvdijke/synapse/commit/95677d89c2bab69b2ec4d41d8bfdf11f5f0e406e))
* improve healthcheck parser, add periodic sync scheduler, add client tests ([4903814](https://github.com/martynvdijke/synapse/commit/4903814e731a9db61f6b65d7870041c6d690db53))

## [1.0.4](https://github.com/martynvdijke/synapse/compare/v1.0.3...v1.0.4) (2026-06-07)


### Bug Fixes

* **deps:** update all non-major dependencies to v1.14.45 ([#6](https://github.com/martynvdijke/synapse/issues/6)) ([8ecfb3e](https://github.com/martynvdijke/synapse/commit/8ecfb3e2aab6da995887e870d4fe5398ce11629d))

## [1.0.3](https://github.com/martynvdijke/synapse/compare/v1.0.2...v1.0.3) (2026-06-04)

## [1.0.2](https://github.com/martynvdijke/synapse/compare/v1.0.1...v1.0.2) (2026-05-29)


### Bug Fixes

* **deps:** update all non-major dependencies ([#4](https://github.com/martynvdijke/synapse/issues/4)) ([de57607](https://github.com/martynvdijke/synapse/commit/de5760744aeac6a051b6ae9f4c04920be54feb89))

## [1.0.1](https://github.com/martynvdijke/synapse/compare/v1.0.0...v1.0.1) (2026-05-19)

# 1.0.0 (2026-05-19)


### Features

* add OpenTelemetry tracing instrumentation ([d75ff90](https://github.com/martynvdijke/synapse/commit/d75ff908d6d9115e2ef69e627d3cf3910e664fa0))
* get in there ([a97fcbb](https://github.com/martynvdijke/synapse/commit/a97fcbb9dec9d802d37beebd895ab3c4084cd0e8))
* rewrite into synapse ([26d10d8](https://github.com/martynvdijke/synapse/commit/26d10d8f78287e53db09ab5684d828d338911445))
