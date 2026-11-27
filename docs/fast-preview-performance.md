# Fast preview performance

The comparison uses baseline `6271ab8`, Go 1.27.3, and eight CPU cores on a Ryzen 9 7950X. The benchmarks use seed `123456`. The table reports medians from five alternating runs per build, with a fresh renderer cache for each cold request.

| Planet | 512×512 cold full frame | Less time | 512×512 cold AVIF tile | Less time |
| --- | ---: | ---: | ---: | ---: |
| Nauvis | 217 → 141 ms | 35% | 292 → 214 ms | 26% |
| Vulcanus | 694 → 157 ms | 77% | 853 → 295 ms | 65% |
| Gleba | 383 → 154 ms | 60% | 473 → 245 ms | 48% |
| Fulgora | 571 → 121 ms | 79% | 534 → 158 ms | 70% |
| Aquilo | 787 → 36 ms | 95% | 859 → 85 ms | 90% |

Both columns use one world tile per pixel. The AVIF column measures rendering and encoding in process. At four world tiles per pixel, cold AVIF tiles took about 30–78% less time across the five planets, based on four runs per build. Aquilo went from 3.72 s to 0.82 s at that scale. At 1024×1024 and one tile per pixel, cold full frames took 37–94% less time across three runs per build.

A repeat Nauvis tile through the in-process HTTP handler went from 635 to 79 µs. Across the five planets, the render and encode path for an identical warm request went from 0.54–0.58 ms to 1.6–1.7 µs. Warm raw frames allocate and copy about 1.05 MB of RGBA data. Their median times ranged from 0.06–0.14 ms on the baseline to 0.06–0.09 ms in the worktree.

In the eight-tile Nauvis test with four render slots, time to the first tile went from 485 to 274 ms and time for the full batch went from 1.08 s to 0.63 s. A 128-pixel horizontal pan across a 1024×1024 Nauvis view went from 184 to 99 ms.

Terrain and entity placement share samples at unit zoom. Cliff workers compute independent chunks in parallel and assemble them in order. Vulcanus reuses neighboring basalt samples. Aquilo and Fulgora reuse their Voronoi lattice points, and Aquilo retains entity chunks in the existing bounded cache. Gleba's cold full-frame allocations fell from about 70.1 MB to 6.6 MB per render.

Repeat AVIF requests look up encoded bytes through a bounded request index backed by the 32 MiB encoded-image cache. Concurrent requests for the same pixels share an encode.

## Image verification

- All 30 Factorio 2.1.23 comparisons produced black diff PNGs with zero total RGB error across terrain, cliffs, and full previews.
- All 10 independent Factorio holdout renders matched the saved RGBA hashes.
- Another 270 Fast renders matched the starting build byte for byte, including custom map controls, large coordinates, panned and cached views, and zoom levels up to 64 tiles per pixel.
- A regression test checks Aquilo's entity draw order at maximum zoom, where footprints from different chunks can paint the same screen pixel.
- The tile-stitching tests checked adjoining browser tiles against a single render. Cache and encoder tests also exercised concurrent requests and cancellation.
- The full Go suite passed with the Factorio reference binary and in a separate race-detector run. All 31 JavaScript tests passed.

## Reproduce

Run the benchmarks on both builds for a direct comparison. Copy the benchmark file into a temporary checkout of the baseline if measuring that revision. [Benchmark source](../fast_preview_performance_test.go)

```bash
go test -run '^$' -bench '^BenchmarkPerformanceFrames$/./^512$/^cold$' -benchtime=3x -count=5 -benchmem
go test -run '^$' -bench '^BenchmarkPerformanceBrowser$/./^zoom-1$/^cold$' -benchtime=3x -count=5 -benchmem
go test -run '^$' -bench '^BenchmarkPerformanceHTTPWarm$' -benchtime=300ms -count=5 -benchmem
FACTMAPGEN_ACCURACY_ARTIFACTS=1 go test -run '^TestPlanet(ReleaseAccuracy|StandaloneHoldout)$' -count=1 -v
```
