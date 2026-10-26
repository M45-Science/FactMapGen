// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"math"
	"strings"
)

func (e *factorioSpaceAgeEvaluator) cliffField() func(float64, float64) factorioCliffCorner {
	s := e.settings
	richness := float32(s.cliffRichness * s.cliffs.size)
	term := float32(.5 * math.Log2(float64(richness)))
	quick := makeFactorioQuickNoise32(s.seed, 123, 2, 1.0/32, 1, 1.0/3, 1, 0, false)
	return func(x, y float64) factorioCliffCorner {
		xf, yf := float32(x), float32(y)
		basic := glebaClamp(term+quick(xf, yf), 0, 1) + .5
		switch s.planet {
		case fastPreviewPlanetVulcanus:
			return factorioCliffCorner{elevation: e.vulcanus.sampleWithStep(x, y, 4).elevation, cliffiness: float64(basic)}
		case fastPreviewPlanetGleba:
			v := e.gleba.sample(x, y)
			return factorioCliffCorner{elevation: float64(v.elevation), cliffiness: float64(max(.95*basic, .99*noiseBool(v.rockpools > 0)))}
		case fastPreviewPlanetFulgora:
			f := e.fulgora
			v := f.sample(x, y)
			mask := glebaClamp(f.influence(xf, yf)+.6, 0, 1)
			start := factorioNearestStart(s.startingPositions, x, y)
			sx, sy := xf-float32(start.x), yf-float32(start.y)
			safe := noiseBool(sx < f.grid && sx > -f.grid && sy < 5 && sy > -5)
			cliffy := 8*noiseSliderRescale(richness, 20)*(f.roads.sample(xf, yf, true).pyramid-.1+max(0, 1-10*mask)) - 1000*safe
			return factorioCliffCorner{elevation: float64(v.elevation), cliffiness: float64(cliffy)}
		}
		return factorioCliffCorner{}
	}
}

func factorioNearestStart(points []factorioPoint, x, y float64) factorioPoint {
	var best factorioPoint
	distance := math.Inf(1)
	for _, p := range points {
		d := (x-p.x)*(x-p.x) + (y-p.y)*(y-p.y)
		if d < distance {
			best, distance = p, d
		}
	}
	return best
}

func (e *factorioSpaceAgeEvaluator) cliffTileCollides(x, y float64) bool {
	switch e.settings.planet {
	case fastPreviewPlanetVulcanus:
		return strings.HasPrefix(e.vulcanus.sample(x, y).tile.name, "lava")
	case fastPreviewPlanetGleba:
		n := e.gleba.sample(x, y).tile.name
		return strings.HasPrefix(n, "wetland-") || n == "gleba-deep-lake"
	case fastPreviewPlanetFulgora:
		return false // Fulgora cliffs omit water_tile, and oil oceans have no player/floor layer.
	}
	return false
}

func (e *factorioSpaceAgeEvaluator) cliffCells(ctx context.Context, x0, y0, x1, y1 float64) ([]factorioCliffCell, error) {
	s := e.settings
	if s.planet == fastPreviewPlanetAquilo || !s.cliffs.enabled || s.cliffs.size <= 0 || s.cliffRichness <= 0 {
		return nil, nil
	}
	tileCache := map[[2]int]bool{}
	reject := func(c factorioCliffCell) bool {
		box := factorioCliffCollisionBoxes[factorioCliffCellCode(c.left, c.right, c.top, c.bottom)]
		for y := int(math.Floor(c.y + box[1])); y <= int(math.Floor(c.y+box[3])); y++ {
			for x := int(math.Floor(c.x + box[0])); x <= int(math.Floor(c.x+box[2])); x++ {
				if len(tileCache) > 4096 {
					clear(tileCache)
				}
				key := [2]int{x, y}
				blocked, ok := tileCache[key]
				if !ok {
					blocked = e.cliffTileCollides(float64(x), float64(y))
					tileCache[key] = blocked
				}
				if blocked {
					return true
				}
			}
		}
		return false
	}
	return factorioPlaceCliffs(ctx, x0, y0, x1, y1, s.cliffElevation0, s.cliffElevationInterval/positiveOr(s.cliffs.frequency, 1), s.cliffSmoothing, e.cliffField(), reject)
}

func (e *factorioSpaceAgeEvaluator) cliffPixels(ctx context.Context, width, height int, x0, y0, tpp float64) ([]bool, error) {
	cells, err := e.cliffCells(ctx, x0-4, y0-4, x0+float64(width)*tpp+4, y0+float64(height)*tpp+4)
	if err != nil {
		return nil, err
	}
	pixels := make([]bool, width*height)
	for _, c := range cells {
		func(l, t, r, b float64) {
			lx, hx := factorioCliffPixelSpan(l, r, x0, tpp)
			ly, hy := factorioCliffPixelSpan(t, b, y0, tpp)
			for y := max(0, ly); y <= min(height-1, hy); y++ {
				for x := max(0, lx); x <= min(width-1, hx); x++ {
					pixels[y*width+x] = true
				}
			}
		}(factorioCliffChartRect(c))
	}
	return pixels, nil
}
