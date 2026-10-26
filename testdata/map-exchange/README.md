# Native map exchange regression

`chatwire-2.1.23.txt` is the Factorio 2.1.23-2 exchange payload from a failing
SoftMod / ChatWire preview link. Before the parser fix, it reported
`unexpected data after map exchange payload: 155 bytes`.

`chatwire-2.1.23.parsed.json` was captured independently using Factorio 2.1.23
headless with Space Age enabled, calling:

```lua
helpers.table_to_json(helpers.parse_map_exchange_string(exchange))
```

The regression compares all map settings against this native decoder output,
including the removed pollution field and added expansion dispatch cooldown.
The native decoder omits internal map-generation fields such as the generation
bounding box, so map-generation comparisons cover the seed and resource controls.
