# Licensing and source attribution

## Project license

The metaX project keeps **Apache License 2.0** for project-authored code and its MetaTube-derived foundation. The MetaTube SDK source repository is also Apache-2.0; copied SDK code must retain its applicable copyright and license notices.

## Amane reference

Amane ([sqzw-x/amane](https://github.com/sqzw-x/amane)) is licensed under GPL-3.0. We consult it as a functional reference to identify site coverage, fields, edge cases, and behavior worth reproducing in native MetaTube Providers.

This Apache-2.0 project does **not** treat an Amane attribution as permission to relicense Amane source code. Do not paste, translate, or substantially adapt its source files or copy its test fixtures into this repository while claiming those items are Apache-only. The native Go Providers in metaX must be authored for MetaTube's public Provider interfaces and documented site behavior, and must not vendor Amane implementation source or fixtures.

If a future change intentionally ports Amane source code or is determined to be a derivative implementation of it, stop and record the affected files and applicable terms before merging or distributing. That change may require a GPL-3.0-compatible distribution strategy or an explicit license grant from the relevant copyright holders. Do not silently change the repository-wide license; the project owner should decide the scope.

## Attribution and third-party notices

- Amane's role as a functional reference is acknowledged in [NOTICE.md](../NOTICE.md) and relevant Provider comments.
- MetaTube SDK code reused in this project remains subject to Apache-2.0 and its required notices.
- New site parsers and their tests should be independently authored. Do not include captured fixtures from upstream projects unless their license and redistribution terms have been reviewed.

This file documents the project's intended source-reuse policy; it is not a legal opinion about any particular implementation.
