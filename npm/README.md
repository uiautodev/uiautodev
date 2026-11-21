# uiautodev

[![npm version](https://img.shields.io/npm/v/uiautodev.svg)](https://www.npmjs.com/package/uiautodev)

CLI for [UIAutoDev](https://github.com/uiautodev/uiautodev), a tool focused on **mobile device control, automation, and remote real devices**. It supports Android, iOS, and HarmonyOS.

This package only contains the npm launcher. It downloads the prebuilt UIAutoDev server binary for your platform and runs it, so you can get started without installing anything:

```bash
npx uiautodev
```

After startup, the server listens on `http://127.0.0.1:33299` by default.

> The desktop app and server live in the same repository; this package is the supported `npx uiautodev` entry point.

## CLI

| Command / Flag | Description |
|---|---|
| `run` | Download (if needed) and run the server binary (default command); extra args are passed through as-is |
| `download` | Only download; print the binary path |
| `path` | Only print the binary path; do not download |
| `create <name>` | Create a new plugin project from a template (`-t, --template <t>` to pick a template, `-f, --force` to replace an existing directory) |
| `--version <v>` | Specify a version (default: latest) |
| `--force` | Ignore cache and force re-download |
| `--debug` | Output debug logs (equivalent to `DEBUG=uiautodev:*`) |
| `--help` | Show help |

The binary is cached at `~/.cache/uiautodev/<version>/`. Override with the `UIAUTODEV_CACHE_DIR` environment variable.

```bash
npx uiautodev                       # download the latest server and run it
npx uiautodev run -addr :8000       # run and pass through args (listen on :8000)
npx uiautodev --version 0.6.0 run   # specify a version
npx uiautodev download              # only download; print the binary path
npx uiautodev path                  # only print the binary path; do not download
npx uiautodev create my-plugin      # scaffold a plugin project from a template
```

> Note: `run` passes all arguments after the subcommand through to the server binary as-is, so global options like `--debug` and `--force` must be placed before the subcommand.

## Feedback

Feedback, issues, and feature requests for UIAutoDev are welcome in [GitHub Issues](https://github.com/uiautodev/uiautodev/issues). Please tell us which platform and version you are using, and include logs when possible.

## Related projects and plugins

Plugin repositories live at [github.com/uiautodev-plugins](https://github.com/uiautodev-plugins).

- [uiautodev-plugins](https://github.com/uiautodev-plugins) Official plugin templates and plugins
- [go-adbkit](https://github.com/codeskyblue/go-adbkit) A pure Go client library for the Android Debug Bridge (ADB) protocol
- [go-hdckit](https://github.com/uiautodev/go-hdckit) A pure Go client and CLI for HarmonyOS HDC
- [dictlog](https://github.com/uiautodev/dictlog) A structured Python logging library, compatible with the standard `logging` module

## Download

Prebuilt packages can be downloaded from [https://get.uiauto.dev](https://get.uiauto.dev).

## Documentation

https://www.yuque.com/codeskyblue/uiautodev

## License

Released under the [MIT License](https://github.com/uiautodev/uiautodev/blob/master/LICENSE).
