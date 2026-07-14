# fcopy

A fast, cross-platform file copy tool for linux and windows.

- Parallel copying for directories with many files
- Native OS copy operations
- Repeatable exclusion patterns
- Preserves directory structure and symbolic links

## Install

Download a pre-built binary from the [releases](https://github.com/coalaura/fcopy/releases/latest) page.

## Usage

```sh
fcopy [options] SOURCE DESTINATION
```

Exclude files or directories by repeating `--exclude`:

```sh
fcopy \
  --exclude "*.tmp" \
  --exclude ".git" \
  --exclude "build/**" \
  source destination
```

Set the maximum number of concurrent file copies:

```sh
fcopy --workers 16 source destination
```

Run `fcopy --help` for all available options.

## Build

Requires Go 1.26 or later.

```sh
go build .
```

## License

See [LICENSE](LICENSE).