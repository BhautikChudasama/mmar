> WARNING: This is still in Alpha stage, not ready for production use yet.

<p align="center">
    <picture>
      <img alt="mmar" title="mmar" src="./docs/assets/img/mmar-gopher-logo.png">
    </picture>
</p>

# mmar

mmar (pronounced "ma-mar") is a zero-dependancy, self-hostable, cross-platform HTTP tunnel that exposes your localhost to the world on a public URL.

It allows you to quickly share what you are working on locally with others without the hassle of a full deployment, especially if it is not ready to be shipped.

<!-- screenshot/gif of mmar in action -->

### Key Features

- Super simple to use
- Utilize "mmar.dev" to tunnel for free on a generated subdomain
- Expose multiple ports on different subdomains
- Live logs of requests coming into your localhost server
- Zero dependancies
- Self-host your own mmar server to have full control

### Learn More

The development, implementation and technical details of mmar has all been documented in a [devlog series](https://ymusleh.com/tags/mmar.html). You can read more about it there.

_p.s. mmar means “corridor” or “pass-through” in Arabic._

## Installation

### MacOS

Use [Homebrew](https://brew.sh/) to install `mmar` on MacOS:

```
brew install yusuf-musleh/mmar-tap/mmar
```

### Docker

The fastest way to create a tunnel what is running on your `localhost:8080` using [Docker](https://www.docker.com/) is by running this command:

```
docker run --rm --network host ghcr.io/yusuf-musleh/mmar:v0.1.6 client --local-port 8080
```

### Linux

TBD -- see Docker or Manual installation instructions for now

### Windows

TBD -- see Docker or Manual installation instructions for now

### Manually

Download a [Release](https://github.com/yusuf-musleh/mmar/releases/) from Github that is compatible with your OS, extract/locate the `mmar` binary and add it somewhere in your PATH.

## Quick Start

1. Check that you have `mmar` installed

```
$ mmar version
mmar version 0.1.6
```
1. Make sure you have your localhost server running on some port (eg: 8080)
1. Run the `mmar` client, pointing it to your localhost port
```
$ mmar client --local-port 8080

2025/02/02 16:26:54 Starting mmar client...
  Creating tunnel:
    Tunnel Host: mmar.dev
    Local Port: 8080

2025/02/02 16:26:54 Tunnel created successfully!

A mmar tunnel is now open on:

>>>  https://7v0aye.mmar.dev -> http://localhost:8080
```
1. That's it! Now you have an HTTP tunnel open through `mmar.dev` on a randomly generated unique subdomain
1. Access this link from anywhere and you should be able to access your localhost server
1. You can see all the options `mmar` by running the help command:
```
$ mmar --help
mmar is an HTTP tunnel that exposes your localhost to the world on a public URL.

Usage:
  mmar <command> [command flags]

Commands:
  server
    Runs a mmar server. Run this on your publicly reachable server if you're self-hosting mmar.
  client
    Runs a mmar client. Run this on your machine to expose your localhost on a public URL.
  version
    Prints the installed version of mmar.


Run `mmar <command> -h` to get help for a specific command
```

## Self-Host

TBD

## License

[AGPL-3.0](https://github.com/yusuf-musleh/mmar#AGPL-3.0-1-ov-file)

## Attributions

Attributions for the mmar gopher logo:

- [gopherize.me](https://gopherize.me/)
- <a href="https://www.vecteezy.com/free-vector/icons">Icons Vectors by Vecteezy</a>
