# Changelog

## 1.0.0 (2026-02-27)


### Features

* add version embedding and optimize nix build ([c997f0a](https://github.com/lukasngl/valet/commit/c997f0ac72daaadd3965aaafb3b8c978a408ac41))
* **framework:** add instrumented provider decorator with metrics and logging ([#3](https://github.com/lukasngl/valet/issues/3)) ([ebb1214](https://github.com/lukasngl/valet/commit/ebb1214946d4e637523d8ffb8cd86842f2d81091))
* implement secret manager operator with Azure adapter ([40b7e64](https://github.com/lukasngl/valet/commit/40b7e64ed6d61c16af7ebf6ca0ad66fb349fba85))
* split into framework + per-provider modules ([f6492af](https://github.com/lukasngl/valet/commit/f6492af61f3dda13b366375a7a48ecfaa69cea36))


### Bug Fixes

* **azure:** align test env var with CI secrets ([66c4615](https://github.com/lukasngl/valet/commit/66c461519b176f8465c861efa53d4c281d0023a0))
* **ci:** include framework in provider coverage via -coverpkg ([c348698](https://github.com/lukasngl/valet/commit/c3486980b535d4cfa16c01db699862e665930b8e))
* go mod tidy, remove redundant replace, lint fixes ([9b206ce](https://github.com/lukasngl/valet/commit/9b206ce22927348dca9dc3e8b723cbb356c0e134))
* make CRD schema output deterministic ([58d8bd6](https://github.com/lukasngl/valet/commit/58d8bd69ea79b956fdd15b67cde9518049b4c4f3))
* remove tenantId from config, auto-copy CRD to helm chart ([7083d0a](https://github.com/lukasngl/valet/commit/7083d0acc8e5846fe8b8762496feba2dcd8c2ea8))
