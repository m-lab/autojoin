
| branch | coveralls | docs | report card |
|--------|-----------|------|-------------|
| main   | [![Coverage Status](https://coveralls.io/repos/github/m-lab/autojoin/badge.svg?branch=main)](https://coveralls.io/github/m-lab/autojoin?branch=main) | [![GoDoc](https://godoc.org/github.com/m-lab/autojoin?status.svg)](https://godoc.org/github.com/m-lab/autojoin) | [![Go Report Card](https://goreportcard.com/badge/github.com/m-lab/autojoin)](https://goreportcard.com/report/github.com/m-lab/autojoin)

# Autojoin API

## List Nodes

The Autojoin API allows listing all known servers for various reasons:

### Request Parameters

Node list operations use the same base url and supports multiple output formats.

Base: `https://autojoin.measurementlab.net/autojoin/v0/node/list`

Formats:

* `format=script-exporter` - output format used by script-exporter.
* `format=prometheus` - output format used by prometheus to scrape metrics.
* `format=servers` - simple list known server names.
* `format=sites` - simple list known site names.
* `org=<org>` - limit results the given organization.

For example, a client could list all known sites associated with org "foo":

* `https://autojoin.measurementlab.net/autojoin/v0/node/list?format=sites&org=foo`
