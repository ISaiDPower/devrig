# DevRig

DevRig installs and runs native development environments: PHP with extensions, Nginx or Apache, Mailpit, Node.js, Go, Rust, Python and more. No containers, no virtual machines.

It comes in two flavours that share the same engine:

- **DevRig**, a desktop app
- **rig**, a command line tool

> **Status:** early development. Nothing is usable yet.

## The idea

Describe a project's environment in a `devrig.toml` file committed next to the code:

```toml
[runtimes]
php  = { version = "8.3", extensions = ["redis", "imagick", "xdebug"] }
node = "22"

[services]
nginx   = {}
mailpit = {}
mysql   = "8.4"

[site]
domain = "shop.test"
root   = "public"
https  = true
```

Then run `rig up`. DevRig installs whatever is missing, starts the services, sets up the local domain with HTTPS and gets out of the way. A teammate who clones the repository gets the exact same environment with the same command.

The format above is a draft and will change.

## Platforms

Windows comes first. macOS and Linux will follow.

## Building from source

You need [Go](https://go.dev/dl/) (see `go.mod` for the version).

```sh
go build -o bin/ ./cmd/rig
bin/rig version
```

## License

[MIT](LICENSE)
