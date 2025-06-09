<br/>
<p align="center">
  <h3 align="center">Nahida Desktop</h3>

  <p align="center">
    Desktop client for nahida.live
    <br/>
    <br/>
  </p>
</p>

### Installation and building

1. Clone repository

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
```

2. Update dependencies

```sh
cd nahida-desktop && bun install
```

3. Running a development build

To run a development

```sh
bun dev
```

4. Build

```sh
bun run build:win

Building the client requires setting up signing and notarization. See "build/" directory and package.json key.
```

## License

Distributed under the Apache-2.0 license.
