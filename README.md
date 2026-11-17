# go-mcjava

The Minecraft Java Edition protocol in Go, written for **crossplay**: Java Edition players on
Bedrock servers. It is the Java protocol library of:

- [Crossfly](https://github.com/ezchr/crossfly): Java and Bedrock players natively on one Dragonfly server.
- [Delta](https://github.com/ezchr/delta): an Endstone plugin that lets Java players play on BDS servers.

The packages have no Dragonfly or Bedrock dependency and can be used on their own.

Java **26.3** (protocol 777) and **26.2** (protocol 776) clients are supported. A server is written
against 26.3; the `version` package converts its ids for 26.2 clients.

It is written for speed and to sit inside a server: framing does not allocate, buffers are
bounds-checked, and the encoders are checked byte for byte against vanilla.

| Package | What it does |
|---|---|
| `wire` | Data types, packet framing, compression and encryption |
| `v777`, `v776` | Packet ids, registries and configuration packets per version, generated from Mojang's data reports and a vanilla join |
| `version` | Maps the ids a 26.3 server writes to an older client's (packets, block states, registries) |
| `server` | Handshake, status, offline and online (Microsoft) login, Velocity modern forwarding, login cookies, and configuration, with per-IP and auth limits |
| `chunk` | `level_chunk_with_light` encoding and decoding |
| `item` | Item stacks (slots) with data component patches, per version: the wire form of every data component (122 in 26.3), so any stack is read and written again byte for byte, and for a 26.2 client with its ids and forms; typed values for the ones servers set (profile, banner patterns, trim, fireworks, books, containers, attribute modifiers, food, tools...) |
| `text` | Network NBT text components and legacy colour codes |
| `textcolour` | Bedrock's colour code palette and colour gradients (`<gradient:#FF2020:&6>text</gradient>`) for either edition |
| `cmd/javagen`, `cmd/remapgen` | Generate the `vNNN` packages and the remap tables from Mojang's reports |
| `cmd/jbot`, `cmd/jbot262`, `cmd/jflat`, `cmd/chunkdiff` | Scripted test clients (26.3 and 26.2), a flat test server, and a chunk comparison tool |

## Licence

MIT. The AES/CFB8 cipher in `internal/cfb8` comes from [Tnze/go-mc](https://github.com/Tnze/go-mc)
(MIT). Data in `v777` and `v776` is generated from Mojang's server reports.
