<!--
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2025-10-04 20:43:45
 * @LastEditTime: 2025-10-06 13:42:03
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /utah/README.md
-->

# utah

Uiharu Tape Archive Helper

An interactive tool to help you to manage tape archives. A libre software under the AGPLv3 license.

## Features

- Generate machine-readable and also human-friendly **manifest** JSON file contains metadata of archives.
- Export human-friendly printable **catalog** file.
- With site, tape ID (usually barcode), file index, you can easy to find the archive files on tapes.
- With tags, extra attributes, notes, it's easy to find the archive you want.
- With SHA-256 checksum, you can validate your archive files.
- Auto suffix check can help you prevent some careless mistakes.
- Go to shell any time you want.
- Zero-dependency design, easy to build using official Go toolchain, even without Internet connection.
- Good for small studios, personal tape archive, or small-scale cold storage.

## How to build

```bash
git clone https://github.com/FunctionSir/utah.git
cd utah
go build
```

## How to use

Just:

```bash
utah PATH_OF_MANIFEST_JSON
```

File not exists means create a new manifest.

Then you can just follow the instructions.
