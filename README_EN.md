> **中文文档：** [README.md](README.md)

## Introduction

[![npm version](https://img.shields.io/npm/v/uiautodev.svg)](https://www.npmjs.com/package/uiautodev)

uiautodev is a tool focused on **mobile device control, automation, and remote real devices**. It comes in two forms: a desktop app and a server. It supports Android, iOS, and HarmonyOS, helping developers and testers efficiently handle device management and UI automation.

> **What this repository contains:** only the npm-related code for UIAutoDev — the `uiautodev` CLI that downloads and runs the server binary, and provides the `npx uiautodev` entry point. The UIAutoDev core (desktop app and server) is **not open source yet**. Many related projects and plugins are open source, see [Related projects and plugins](#related-projects-and-plugins).

![uiautodev](images/uiautodev.jpg)

## Quick Start

No installation required. Copy and run the command below (it will automatically download the server binary for your platform on first run):

```bash
npx uiautodev
```

After startup, the server listens on `http://127.0.0.1:33299` by default.

## Repository Layout

| Path | Description |
|---|---|
| `npm/` | The published `uiautodev` npm package: CLI source, tests, and package metadata |
| `images/` | Images used in the documentation |

The UIAutoDev core is not part of this repository and is not open source yet.

## Releasing

Pushing to `master` triggers the [Release workflow](.github/workflows/release.yml). It runs the tests, bumps the version in `npm/package.json`, commits and tags the release, creates a GitHub release, and triggers the [npm publish workflow](.github/workflows/publish.yml). The bump is inferred from [Conventional Commits](https://www.conventionalcommits.org/) since the last version bump: `feat` → minor, `fix`/`perf`/`revert` → patch, and a breaking change → major. Commits that are not version-worthy (`docs`, `chore`, `ci`, …) do not release.

You can also run the Release workflow manually and choose the bump. Add `[skip release]` to a commit message to skip it.

## CLI

| Command / Flag | Description |
|---|---|
| `run` | Download (if needed) and run the server binary (default command); extra args are passed through as-is |
| `download` | Only download; print the binary path |
| `path` | Only print the binary path; do not download |
| `create <name>` | Create a new plugin project from a template (prompts for the template; use `-t, --template <t>` to skip, `-f, --force` to replace an existing directory) |
| `--version <v>` | Specify a version (default: latest) |
| `--force` | Ignore cache and force re-download |
| `--debug` | Output debug logs (equivalent to `DEBUG=uiautodev:*`, e.g. request/response for download stats POST) |
| `--help` | Show help |

The binary is cached at `~/.cache/uiautodev/<version>/`. Override with the `UIAUTODEV_CACHE_DIR` environment variable.

```bash
npx uiautodev                       # download the latest server and run it
npx uiautodev run -addr :8000       # run and pass through args (listen on :8000)
npx uiautodev --version 0.6.0 run   # specify a version
npx uiautodev download              # only download; print the binary path
npx uiautodev download --force      # force re-download
npx uiautodev path                  # only print the binary path; do not download
npx uiautodev create my-plugin      # scaffold a plugin project from a template
npx uiautodev create my-plugin -t preact              # pick a template explicitly
npx uiautodev create my-plugin -t preact --force      # replace an existing directory
npx uiautodev --debug download      # debug mode: view request/response for download stats POST
DEBUG=uiautodev:* npx uiautodev download   # same as above, enable debug logging via env var
```

> Note: `run` passes all arguments after the subcommand through to the server binary as-is, so global options like `--debug` and `--force` must be placed before the subcommand (e.g. `npx uiautodev --debug run`).

## Download

Prebuilt packages can be downloaded from:

[https://get.uiauto.dev](https://get.uiauto.dev)

## Documentation

https://www.yuque.com/codeskyblue/uiautodev

## Feedback

We collect feedback for UIAutoDev in [GitHub Issues](https://github.com/uiautodev/uiautodev/issues). Bug reports, feature requests, plugin ideas, and general suggestions are all welcome. Please include your platform, the UIAutoDev version, and relevant logs when possible.

## Related Projects and Plugins

The core is not open source, but many related projects and plugins are. Plugin repositories and templates live at [github.com/uiautodev-plugins](https://github.com/uiautodev-plugins).

- [uiautodev-plugins](https://github.com/uiautodev-plugins) Official open-source plugin templates and plugins
- [dictlog](https://github.com/uiautodev/dictlog) A structured Python logging library, compatible with the standard `logging` module
- [uiautoagent](https://github.com/uiautodev/uiautoagent) Use AI to control your phone to complete tasks

## License

Released under the [MIT License](LICENSE).
