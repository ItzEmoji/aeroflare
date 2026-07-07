# Changelog

## [1.7.0](https://github.com/ItzEmoji/aeroflare/compare/v1.6.0...v1.7.0) (2026-07-07)


### Features

* core CacheBackend interface and factory ([58f70a4](https://github.com/ItzEmoji/aeroflare/commit/58f70a42844982b390cdb9c92d261c889414b6af))
* introduce generic OCI utilities for Aeroflare annotations ([c7ba176](https://github.com/ItzEmoji/aeroflare/commit/c7ba1769cb6687f0139c7f289c2cc2d02eb6dc24))
* json CacheBackend implementation ([2408ec8](https://github.com/ItzEmoji/aeroflare/commit/2408ec8baf7578a7929293b7dafa942e6fa33ce8))
* native CacheBackend implementation ([2c9365c](https://github.com/ItzEmoji/aeroflare/commit/2c9365c42d0b35e8abe0cea83059e718813a1d40))
* r2 CacheBackend implementation with chunked OCI manifest ([bc08427](https://github.com/ItzEmoji/aeroflare/commit/bc084270d814a2a5dfd18ed01b4ae22427d4d9e5))


### Bug Fixes

* address reviewer feedback for native backend ([37882e0](https://github.com/ItzEmoji/aeroflare/commit/37882e0d9d8b7391a3ab0c2d56cf2aa2bb1793c6))
* address reviewer feedback for r2 backend ([f7a7d6a](https://github.com/ItzEmoji/aeroflare/commit/f7a7d6af1bd67c13d88b89a127630268fbb718a1))
* fixed 401-errors with the registry. ([e09ea9c](https://github.com/ItzEmoji/aeroflare/commit/e09ea9cbd10c8d55bf64e1c42a2dcd11883c0800))
* fixed layout ([f86cff7](https://github.com/ItzEmoji/aeroflare/commit/f86cff7517d049d54870b156f7f8eaca7e93ddf7))
* handle json backend issues from review ([c3233ad](https://github.com/ItzEmoji/aeroflare/commit/c3233ad8447e8c58c3b1a8d6f6f7c2ab486765b8))
* harden network, index, and push paths against partial failures ([f93bd14](https://github.com/ItzEmoji/aeroflare/commit/f93bd1458e5944df4a5921b8e975c6316ac5b989))
* make generated CLI reference MDX-safe ([38cea0f](https://github.com/ItzEmoji/aeroflare/commit/38cea0fcd82e61c71980e1c6d8b7910368167a3b))
* native backend type ([545631c](https://github.com/ItzEmoji/aeroflare/commit/545631c3ef6e6fd7d733630d180f4f9dc79746d6))
* point gen_docs at docs/ and honor output-dir arg ([2293da8](https://github.com/ItzEmoji/aeroflare/commit/2293da85370ee284d77c3bc550e0e2e1047bd264))
* resolve unused variable compilation error in push.go ([d76f25f](https://github.com/ItzEmoji/aeroflare/commit/d76f25fa19ef012f2bcbd05409b5a450391cd6ba))
* robustness improvments ([8ef7c98](https://github.com/ItzEmoji/aeroflare/commit/8ef7c98363db625e6e03be81de82a66ed423c224))

## [1.6.0](https://github.com/ItzEmoji/aeroflare/compare/v1.5.0...v1.6.0) (2026-07-02)


### Features

* docs ([7314018](https://github.com/ItzEmoji/aeroflare/commit/7314018184880e81ae2b679f2f0a41f8186210f3))


### Bug Fixes

* delete default pages and set intro.mdx as homepage to fix build ([a6aa292](https://github.com/ItzEmoji/aeroflare/commit/a6aa292e9f9ad489de70e9b6140dc63041c1198e))

## [1.5.0](https://github.com/ItzEmoji/aeroflare/compare/v1.4.1...v1.5.0) (2026-07-01)


### Features

* add auth CLI commands ([ca68a16](https://github.com/ItzEmoji/aeroflare/commit/ca68a163fc373a0ca83101cf91d97460e5b800f4))
* add auth list advanced parsing, json export, and auth import command ([70e599d](https://github.com/ItzEmoji/aeroflare/commit/70e599d68ff22f680afbdef69b7ea5f1d51285a0))
* add auth subcommands (list, login, remove) and keychain visibility ([d37da75](https://github.com/ItzEmoji/aeroflare/commit/d37da75cb479e333c94580d1337ca6b0895df09b))
* add beautiful nushell-style tables for auth list ([a8b22c3](https://github.com/ItzEmoji/aeroflare/commit/a8b22c32460ce4ed32d0a7435edbba6b3d9ad557))
* add config setup ([ecf2dfd](https://github.com/ItzEmoji/aeroflare/commit/ecf2dfd09a89f16a69f943c85f3cc3db8e89d2bc))
* add settings command ([9687292](https://github.com/ItzEmoji/aeroflare/commit/968729218305457869c15acf4499ecb385791950))
* **auth:** add registry-aware token resolvers ([2f922fc](https://github.com/ItzEmoji/aeroflare/commit/2f922fc37cb1b5418b5247d712d4508280c40be0))
* **auth:** centralize credential resolution logic in auth_resolve.go ([55cc9d5](https://github.com/ItzEmoji/aeroflare/commit/55cc9d571e93bb83579babd5fc560310a4e0550d))
* **auth:** implement GitHub device authorization flow ([2ac2d1d](https://github.com/ItzEmoji/aeroflare/commit/2ac2d1dc43f44fa28db2e7f12e54acc3d4bcfc3c))
* **auth:** implement interactive setup wizard via huh ([0b5c786](https://github.com/ItzEmoji/aeroflare/commit/0b5c786ff6aef23cdce6a93270ad5a8e43db347e))
* **auth:** implement token resolver builder ([67e9738](https://github.com/ItzEmoji/aeroflare/commit/67e9738a4d07a7cc97845e0d810d6047d6af5b1e))
* check and warn if github token is missing write:packages scope ([23a4fc5](https://github.com/ItzEmoji/aeroflare/commit/23a4fc5fe8aacd4f27f1c2fc92e6d8e9adecfe74))
* implement unified secret manager with fallback ([5e03ac0](https://github.com/ItzEmoji/aeroflare/commit/5e03ac05a48c848820632d29ae1e4bbecb086cff))
* initialize viper and auto-generate config ([99f9bd1](https://github.com/ItzEmoji/aeroflare/commit/99f9bd13504b5390501d2fc67156366fb5dac85a))
* integrate secret manager with github token retrieval ([182aa83](https://github.com/ItzEmoji/aeroflare/commit/182aa83f01c264c81274cb707ed14f544659f3d7))
* integrate themes via viper config ([c1a38be](https://github.com/ItzEmoji/aeroflare/commit/c1a38beda9ef37e3511f30f1e59ede4826a69382))
* use viper config to skip wizard prompts ([ffd4a19](https://github.com/ItzEmoji/aeroflare/commit/ffd4a1906e7b0bd3956fb8d8b32b7fdc04779800))


### Bug Fixes

* address final review findings ([addf567](https://github.com/ItzEmoji/aeroflare/commit/addf567543b2e77324704c070b001ee260fe31a6))
* address reviewer feedback for getGithubToken ([3888100](https://github.com/ItzEmoji/aeroflare/commit/3888100b3e1e23f21a131b82aa1c6f105d965e36))
* address reviewer feedback on secrets manager fallback and concurrency ([69fe56e](https://github.com/ItzEmoji/aeroflare/commit/69fe56e23a0ce58c8efb573f98c142c60f52b8ca))
* address task 2 remaining review findings ([60d3ea0](https://github.com/ItzEmoji/aeroflare/commit/60d3ea01cf39c66cfda639b76db470ef0ae77815))
* address task 2 review findings ([f03f9cc](https://github.com/ItzEmoji/aeroflare/commit/f03f9cc04ef0243b2bb1c4eb39f32a5f73c2a701))
* address task 2 review findings (attempt 2) ([b9a6c03](https://github.com/ItzEmoji/aeroflare/commit/b9a6c03a74f497932bc4e81d8ddb2c646ac38881))
* address task 4 review findings ([e9518f6](https://github.com/ItzEmoji/aeroflare/commit/e9518f619c28a78ad6732836d1fad84130d0d786))
* address task 4 review findings (attempt 2) ([78b0211](https://github.com/ItzEmoji/aeroflare/commit/78b021177ccd996f557c4f25ec4cf91c4a142743))
* address task 4 review findings (attempt 3) ([f7ca510](https://github.com/ItzEmoji/aeroflare/commit/f7ca510f5b08e3cd4f3d2b0da741a3fa7a5dfe96))
* address task 4 review findings (attempt 4) ([ea5b228](https://github.com/ItzEmoji/aeroflare/commit/ea5b22891e6a67f58f05f0f0783c2b8761ef5867))
* address task 4 review findings (attempt 5) ([3f3159c](https://github.com/ItzEmoji/aeroflare/commit/3f3159cc3e665ed9a1fb0212271855a894e25282))
* auth wizard error handling and success messages ([8ce9d4d](https://github.com/ItzEmoji/aeroflare/commit/8ce9d4d53cf9342577f7f60dd4d8cbb4d1cab9ff))
* **auth:** add GH_TOKEN test and update signature for OCI generic secret tests ([d2bbdda](https://github.com/ItzEmoji/aeroflare/commit/d2bbdda2d7c701f30a316aedc7690a3d7940ab84))
* **auth:** add workflow scope to interactive github auth login ([1c27414](https://github.com/ItzEmoji/aeroflare/commit/1c274143ebbf43b27622a843d716f41e241dc087))
* **auth:** fix device auth network issues and test races ([e5863fd](https://github.com/ItzEmoji/aeroflare/commit/e5863fd08ed6e99e8ab2ae6771707c28b798e2f9))
* **auth:** handle manager errors, refactor for testability, and add behavioral tests ([06d8c3a](https://github.com/ItzEmoji/aeroflare/commit/06d8c3a2c215fd6875bf2024fa538c5a46f8fb7e))
* **auth:** handle swallowed errors in wizard forms ([af697f7](https://github.com/ItzEmoji/aeroflare/commit/af697f78bf1f4e938aacf68744aa621b9b79f2e3))
* ensure github oauth tokens (gho_) trigger token exchange ([ed50827](https://github.com/ItzEmoji/aeroflare/commit/ed5082770c92753af02559a5076aec4eef69e8e5))
* ensure PATs exported via oci_token are properly exchanged for GHCR ([e313b06](https://github.com/ItzEmoji/aeroflare/commit/e313b062ac187e55c3d408e23d1ec2afbb0c056f))
* export oci_token in getTokenForRegistry so push backend sees the resolved token ([5e5de41](https://github.com/ItzEmoji/aeroflare/commit/5e5de411a27a771ce233cff2749ab566116e9e65))
* fixed support for oci-registry for now. ([96375f9](https://github.com/ItzEmoji/aeroflare/commit/96375f95b7c135e96ab29ebea3921e08e7b0904b))
* implement auth resolver review findings for Task 3 ([fad8711](https://github.com/ItzEmoji/aeroflare/commit/fad8711fdcd842151a785130d3799fce03641b49))
* **init:** add workflow scope to github oauth and warn if missing ([c2f36c3](https://github.com/ItzEmoji/aeroflare/commit/c2f36c3f7ceee76af7d79a401f2b1432721ce0f2))
* **init:** prevent environment variable pollution in setup ([fed0f18](https://github.com/ItzEmoji/aeroflare/commit/fed0f18164551e94d1afbd2f100b96bd261f17e3))
* **init:** update network.GetToken signature and callers ([78f5bf9](https://github.com/ItzEmoji/aeroflare/commit/78f5bf9175151b71428d65b8f06605d87f860341))
* **init:** use auth.Resolver for token detection in wizard ([a09d202](https://github.com/ItzEmoji/aeroflare/commit/a09d2028989661b07ec8fd8ab113d7868212b6cc))
* **init:** use x-access-token for github git push and improve git error logging ([59a586f](https://github.com/ItzEmoji/aeroflare/commit/59a586fa1d28634bd42680d0422a0a61512ea1d0))
* mock keyring in tests and encapsulate ErrNotFound ([7e2d21b](https://github.com/ItzEmoji/aeroflare/commit/7e2d21baf1dc2d85a3ed813da3915304fd7107c2))
* **network:** use registry-aware auth resolver in GetToken ([162713b](https://github.com/ItzEmoji/aeroflare/commit/162713b0bff2478b1084047bf4e14a4c98faef0c))
* **network:** use registry-aware auth resolver in GetToken ([7f5dde1](https://github.com/ItzEmoji/aeroflare/commit/7f5dde1f0e918c757d59f34444ea584a7fc04a62))
* proxy add native type ([#23](https://github.com/ItzEmoji/aeroflare/issues/23)) ([05b7e9c](https://github.com/ItzEmoji/aeroflare/commit/05b7e9c58fefc8926b180be15b519da90d700ac8))
* request write:packages scope in device flow and fix token manager prefix checks ([a772183](https://github.com/ItzEmoji/aeroflare/commit/a7721831ec37abd35d667488551cc26b85df127e))
* restore getGithubToken and token resolution logic ([adb5aec](https://github.com/ItzEmoji/aeroflare/commit/adb5aecf8c3c9521a38efdc817bba3eddc057475))

## [1.4.1](https://github.com/ItzEmoji/aeroflare/compare/v1.4.0...v1.4.1) (2026-06-28)


### Bug Fixes

* push command ([#21](https://github.com/ItzEmoji/aeroflare/issues/21)) ([8d9f81c](https://github.com/ItzEmoji/aeroflare/commit/8d9f81c45ac4d54bd299bad574019dd52db47d2f))

## [1.4.0](https://github.com/ItzEmoji/aeroflare/compare/v1.3.2...v1.4.0) (2026-06-27)


### Features

* add Native OCI Tags backend option ([8ae5bbd](https://github.com/ItzEmoji/aeroflare/commit/8ae5bbde97aef831f9afb5137c7384adc8093dc0))
* added init command, however it's still experimental. ([57cc9d5](https://github.com/ItzEmoji/aeroflare/commit/57cc9d50ff8662e2543c093cf20e2efb60f29992))
* apply custom Aeroflare theme to wizard ([b7050ca](https://github.com/ItzEmoji/aeroflare/commit/b7050ca441237cf2b2ab3349b7857d295269c341))
* automatically set GitHub Actions secrets via API ([84fa582](https://github.com/ItzEmoji/aeroflare/commit/84fa582e8c2296090c13ce1f5a31f743face1fc9))
* implement GitLab device flow authentication ([a5fd401](https://github.com/ItzEmoji/aeroflare/commit/a5fd4015585b1bf1d634f1c68d48aa8487b9bb10))
* integrate GitLab device flow into initialization wizard ([aad0404](https://github.com/ItzEmoji/aeroflare/commit/aad0404407c001d992126bcf1e9e492138281d99))


### Bug Fixes

* added native type to aeroflare init. ([93dfd98](https://github.com/ItzEmoji/aeroflare/commit/93dfd98bee0b76ab2f5ea8854bc3a95f0a0a99d6))
* adjust setup wizard UI and GitLab token logic ([d4d679b](https://github.com/ItzEmoji/aeroflare/commit/d4d679bdf88483e9aecf6fece6afa800db4c4163))
* fixed security vulnerability through coderabbit. ([ea820a3](https://github.com/ItzEmoji/aeroflare/commit/ea820a30e7b6bb47f9aa8a6733a55d30a229181b))
* fixed the uploading for r2 and json. ([13405b0](https://github.com/ItzEmoji/aeroflare/commit/13405b0a36aaa5317a3e06c0b4d176c70bfd304b))
* **gitlab:** handle device flow network errors and add timeout ([37c0864](https://github.com/ItzEmoji/aeroflare/commit/37c086449bb025366adf71cbcc36dfb71852ec81))
* handle OAuth failure fallback in wizard ([b062692](https://github.com/ItzEmoji/aeroflare/commit/b0626927f75e692ddf3c02a72cb7cecbaa9e5ea6))
* **oauth:** add context timeouts for GitLab device flow requests ([09ee7f4](https://github.com/ItzEmoji/aeroflare/commit/09ee7f448a7fb040762fced45d0547ab8c37b426))
* properly format Summary box right border ([69e38e6](https://github.com/ItzEmoji/aeroflare/commit/69e38e67d7acd0cfcf1a1f9a91fa1a99d725f8a6))
* remove unrequested native OCI tags option ([aa3a5a2](https://github.com/ItzEmoji/aeroflare/commit/aa3a5a26c8891c29b31fd0bcc4b51e4b5e586d16))

## [1.3.2](https://github.com/ItzEmoji/aeroflare/compare/v1.3.1...v1.3.2) (2026-06-23)


### Bug Fixes

* general things ([#17](https://github.com/ItzEmoji/aeroflare/issues/17)) ([7de0967](https://github.com/ItzEmoji/aeroflare/commit/7de09670683a0d4b579a2033fc93bdbe7e156e6b))

## [1.3.1](https://github.com/ItzEmoji/aeroflare/compare/v1.3.0...v1.3.1) (2026-06-22)


### Bug Fixes

* general things ([#15](https://github.com/ItzEmoji/aeroflare/issues/15)) ([527f95f](https://github.com/ItzEmoji/aeroflare/commit/527f95fb8c8c3a895d9dbbeb0d09f072c4b0742c))

## [1.3.0](https://github.com/ItzEmoji/aeroflare/compare/v1.2.0...v1.3.0) (2026-06-21)


### Features

* added parrael uploads, added r2 support, and made some other general improvments. ([#14](https://github.com/ItzEmoji/aeroflare/issues/14)) ([f8029a4](https://github.com/ItzEmoji/aeroflare/commit/f8029a421e9625b83ef723cf3054e2ba220a700a))
* made some general fixes and the look of the cli has been improved.  ([#12](https://github.com/ItzEmoji/aeroflare/issues/12)) ([3cc45a2](https://github.com/ItzEmoji/aeroflare/commit/3cc45a21d799b693b7c7e9969e5cf3e236765f2e))

## [1.2.0](https://github.com/ItzEmoji/aeroflare/compare/v1.1.0...v1.2.0) (2026-06-21)


### Features

* narinfo-parser and push ([#10](https://github.com/ItzEmoji/aeroflare/issues/10)) ([7e8bf41](https://github.com/ItzEmoji/aeroflare/commit/7e8bf410f35ae84950e6d2c6d0e64bc405873c48))

## [1.1.0](https://github.com/ItzEmoji/aeroflare/compare/v1.0.0...v1.1.0) (2026-06-19)


### Features

* added proxy-module. ([#5](https://github.com/ItzEmoji/aeroflare/issues/5)) ([135bbaf](https://github.com/ItzEmoji/aeroflare/commit/135bbaf94f0651d05888e75e442748b776bdcc1f))

## 1.0.0 (2026-06-18)


### Features

* Initial Commit. Built the parts in order to upload the files. ([ef6f325](https://github.com/ItzEmoji/aeroflare/commit/ef6f325ab64cd31aab2cb0684bbc1f831fc2f05b))


### Bug Fixes

* lint ([#3](https://github.com/ItzEmoji/aeroflare/issues/3)) ([0ef043b](https://github.com/ItzEmoji/aeroflare/commit/0ef043b29e386fee937225708bbb4b8de56e20ff))
