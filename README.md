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

Use literal recursive basename exclusions with `--exclude-name`:

```sh
fcopy --exclude-name .DS_Store --exclude-name generated.go source destination
```

Control destination collisions:

```sh
fcopy --collision replace source destination
fcopy --collision warn source destination
fcopy --collision fail source destination
```

Follow symbolic links instead of copying the links themselves:

```sh
fcopy --dereference source destination
```

Control copy-on-write cloning:

```sh
fcopy --reflink auto source destination
fcopy --reflink always source destination
fcopy --reflink never source destination
```

Suppress per-file messages while retaining warnings, errors, and the final summary:

```sh
fcopy --quiet source destination
```

Emit a JSON report:

```sh
fcopy --json source destination
```

Run `fcopy --help` for all available options.

## Build

Requires Go 1.26 or later.

```sh
go build .
```

## License

See [LICENSE](LICENSE).