//go:build windows

package main

import (
	"math"
	"testing"
)

func newStereoPhosphorTestState(width, height int) stereoPhosphorState {
	state := stereoPhosphorState{
		width:             width,
		height:            height,
		highlightEnergy:   make([]float32, width*height),
		compositeEnergy:   make([]float32, width*height),
		compositeCoverage: make([]float32, width*height),
		blurScratch:       make([]float32, width*height),
		glowEnergy:        make([]float32, width*height),
		pixels:            make([]byte, width*height*4),
	}
	for i := range state.energyLayers {
		state.energyLayers[i] = make([]float32, width*height)
		state.coverageLayers[i] = make([]float32, width*height)
	}
	state.configureDotGrid()
	return state
}

func TestStereoLineComposeCadenceIsSixtyFPS(t *testing.T) {
	composed := 0
	for tick := uint64(1); tick <= 120; tick++ {
		if stereoLineCompletesFrame(tick) {
			composed++
		}
	}
	if composed != 60 {
		t.Fatalf("120 analysis ticks produced %d LINE frames", composed)
	}
}

func TestStereoPhosphorStylesShareSixtyFPSComposeCadence(t *testing.T) {
	for _, style := range []int{0, 1, 2} {
		state := newStereoPhosphorTestState(31, 31)
		state.dc, state.bitmap = 1, 1
		var sequence uint64
		feed := func(tick uint64) bool {
			points := []StereoPoint{{X: 0.1, Y: 0.2}, {X: -0.2, Y: 0.25}, {X: 0.15, Y: -0.1}, {X: -0.05, Y: -0.3}}
			sequence += uint64(len(points))
			_, didCompose := state.advanceStyle(points, sequence, tick, 0, style, 31, 31)
			return didCompose
		}
		// Warm up the style pipelines: the style-0 curve filter buffers a few
		// control points before its first deposit reaches the surface.
		for tick := uint64(1); tick <= 3; tick++ {
			feed(tick)
		}
		// With content flowing, every style still composes at 60 fps.
		composed := 0
		for tick := uint64(4); tick <= 9; tick++ {
			if feed(tick) {
				composed++
			}
		}
		state.dc, state.bitmap = 0, 0
		if composed != 3 {
			t.Fatalf("style %d produced %d frames from 6 analysis ticks", style, composed)
		}
	}
}

func TestStereoLineFadeLastsThreeHundredMilliseconds(t *testing.T) {
	const start = uint64(100)
	until := stereoLineFadeUntil(start)
	if until-start != 36 {
		t.Fatalf("fade duration = %d analysis ticks, want 36", until-start)
	}
	if !stereoLineFadeRunning(start, until) || !stereoLineFadeRunning(until-1, until) {
		t.Fatal("fade ended before the 300 ms interval")
	}
	if stereoLineFadeRunning(until, until) {
		t.Fatal("fade remained active after the 300 ms interval")
	}
	if displayFrames := (until - start) / stereoLineComposeDivisor; displayFrames != 18 {
		t.Fatalf("fade contains %d display frames, want 18", displayFrames)
	}
}

func TestStereoDotFadeLastsFiveHundredMilliseconds(t *testing.T) {
	const start = uint64(100)
	until := stereoStyleFadeUntil(start, 1)
	if until-start != stereoPhosphorDotFadeTicks {
		t.Fatalf("DOT fade duration = %d analysis ticks, want configured %d", until-start, stereoPhosphorDotFadeTicks)
	}
	if until-start != 60 {
		t.Fatalf("DOT fade duration = %d analysis ticks, want 60", until-start)
	}
	if lineUntil := stereoStyleFadeUntil(start, 0); lineUntil-start != stereoLineFadeTicks {
		t.Fatalf("LINE fade duration changed to %d ticks", lineUntil-start)
	}
}

func TestStereoLineFadeEnergyRemainsMonotonicThroughWindow(t *testing.T) {
	state := newStereoPhosphorTestState(1, 1)
	state.energyLayers[stereoPhosphorCurrentLayer][0] = 0.5
	state.coverageLayers[stereoPhosphorCurrentLayer][0] = 1
	state.ageHistory(1)
	previous := state.energyLayers[stereoPhosphorHistoryLayer][0]
	for frame := uint64(2); frame <= stereoLineFadeTicks/stereoLineComposeDivisor; frame++ {
		state.ageHistory(1)
		current := state.energyLayers[stereoPhosphorHistoryLayer][0]
		if current <= 0 || current >= previous {
			t.Fatalf("fade frame %d energy = %.6f after %.6f", frame, current, previous)
		}
		previous = current
	}
}

func TestStereoLinePairsAnalysisTicksIntoDisplayExposure(t *testing.T) {
	want := []uint64{0, 0, 1, 1, 2, 2}
	for i, expected := range want {
		tick := uint64(i + 1)
		if got := stereoLineDisplayFrame(tick); got != expected {
			t.Fatalf("tick %d display frame = %d, want %d", tick, got, expected)
		}
	}

	state := newStereoPhosphorTestState(1, 1)
	state.moveToDisplayFrame(stereoLineDisplayFrame(1))
	state.energyLayers[0][0] = 1
	state.moveToDisplayFrame(stereoLineDisplayFrame(2))
	if state.energyLayers[0][0] != 1 || state.energyLayers[1][0] != 0 {
		t.Fatal("second analysis tick rotated the current display exposure")
	}
	state.moveToDisplayFrame(stereoLineDisplayFrame(3))
	if state.energyLayers[0][0] != 0 || math.Abs(float64(state.energyLayers[1][0]-stereoPhosphorHistoryInjection)) > 0.000001 {
		t.Fatal("new display frame did not inject the completed exposure into phosphor history")
	}
}

func TestStereoLineCoalescedOddTickComposesLatestFrame(t *testing.T) {
	if stereoLineShouldComposeFrame(3, 2, true) {
		t.Fatal("ordinary first half of the next display frame composed early")
	}
	if !stereoLineShouldComposeFrame(5, 3, true) {
		t.Fatal("coalesced odd tick did not compose the newest available frame")
	}
	if !stereoLineShouldComposeFrame(5, 0, false) {
		t.Fatal("late initial odd tick left the surface empty")
	}
}

func TestStereoLineContinuousHistoryIsDimmerThanCurrent(t *testing.T) {
	if stereoPhosphorHistoryInjection <= 0 || stereoPhosphorHistoryInjection >= 1 ||
		stereoPhosphorHistoryDecay <= 0 || stereoPhosphorHistoryDecay >= 1 {
		t.Fatalf("invalid energy history response: injection %.3f decay %.3f", stereoPhosphorHistoryInjection, stereoPhosphorHistoryDecay)
	}
	if stereoPhosphorCoverageInjection <= 0 || stereoPhosphorCoverageInjection >= 1 ||
		stereoPhosphorCoverageDecay <= 0 || stereoPhosphorCoverageDecay >= 1 {
		t.Fatalf("invalid coverage history response: injection %.3f decay %.3f", stereoPhosphorCoverageInjection, stereoPhosphorCoverageDecay)
	}
	if stereoPhosphorHighlightDecay != 0.82 || stereoPhosphorHighlightAlpha != 0.72 {
		t.Fatalf("invalid fresh highlight response: decay %.3f alpha %.3f", stereoPhosphorHighlightDecay, stereoPhosphorHighlightAlpha)
	}
}

func TestStereoPhosphorSelectsOnlyNewPoints(t *testing.T) {
	var state stereoPhosphorState
	start, discontinuity := state.newPointStart(900, 1000, 1)
	if start != 900-stereoPhosphorInitialMax || !discontinuity {
		t.Fatalf("initial range = %d, %v", start, discontinuity)
	}
	start, discontinuity = state.newPointStart(900, 1012, 1)
	if start != 888 || discontinuity {
		t.Fatalf("incremental range = %d, %v", start, discontinuity)
	}
	start, discontinuity = state.newPointStart(900, 1012, 1)
	if start != 900 || discontinuity {
		t.Fatalf("duplicate snapshot was not skipped: %d, %v", start, discontinuity)
	}
}

func TestStereoPhosphorBreaksContinuityWhenSnapshotHasGap(t *testing.T) {
	state := stereoPhosphorState{lastSequence: 10, sequenceValid: true}
	start, discontinuity := state.newPointStart(64, 1000, 1)
	if start != 0 || !discontinuity {
		t.Fatalf("gapped range = %d, %v", start, discontinuity)
	}
}

func TestStereoPhosphorDelayedFrameKeepsAllContiguousPoints(t *testing.T) {
	state := stereoPhosphorState{lastSequence: 1000, sequenceValid: true}
	start, discontinuity := state.newPointStart(900, 1060, 3)
	if start != 840 || discontinuity {
		t.Fatalf("delayed range = %d, %v; want all 60 contiguous points", start, discontinuity)
	}
}

func TestStereoPhosphorTwoTickCoalescingKeepsFullDisplayExposure(t *testing.T) {
	state := stereoPhosphorState{lastSequence: 1000, sequenceValid: true}
	start, discontinuity := state.newPointStart(900, 1040, 2)
	if start != 860 || discontinuity {
		t.Fatalf("two-tick range = %d, %v; want all 40 contiguous points", start, discontinuity)
	}
}

func TestStereoPhosphorAdvanceKeepsCurveAcrossDelayedBusySnapshot(t *testing.T) {
	state := newStereoPhosphorTestState(96, 96)
	// Mark the test surface as initialized so advance can exercise the geometry
	// path without replacing the in-memory buffers with a Win32 DIB.
	state.dc, state.bitmap = 1, 1
	for i := 0; i < 48; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position: stereoPhosphorPoint{X: 8 + float32(i), Y: 48 + float32(math.Sin(float64(i)*0.18))*10},
			Energy:   0.5,
		}, 2.5)
	}
	before := state.emittedControls
	state.lastSequence, state.sequenceValid = 100, true
	state.lastTick, state.tickValid = 1, true
	state.lastDisplayFrame, state.displayFrameValid = 0, true
	points := make([]StereoPoint, 80)
	for i := range points {
		switch i % 4 {
		case 0:
			points[i] = StereoPoint{X: -0.8, Y: -0.8}
		case 1:
			points[i] = StereoPoint{X: 0.8, Y: -0.8}
		case 2:
			points[i] = StereoPoint{X: 0.8, Y: 0.8}
		default:
			points[i] = StereoPoint{X: -0.8, Y: 0.8}
		}
	}
	ready, _ := state.advance(points, 180, 4, 0, 96, 96)
	if !ready || state.emittedControls <= before {
		t.Fatalf("delayed busy snapshot reset the curve: ready=%v controls %d->%d", ready, before, state.emittedControls)
	}
}

func TestStereoSnapshotReturnsSequence(t *testing.T) {
	engine := AudioEngine{
		stereoPoints:   []StereoPoint{{X: 1}, {X: 2}, {X: 3}},
		stereoSequence: 17,
	}
	var dst [2]StereoPoint
	n, _, sequence := engine.StereoSnapshotInto(dst[:])
	if n != 2 || sequence != 17 || dst[0].X != 2 || dst[1].X != 3 {
		t.Fatalf("snapshot = n:%d sequence:%d points:%v", n, sequence, dst)
	}
}

func TestStereoPhosphorHistoryDecaysSmoothly(t *testing.T) {
	state := newStereoPhosphorTestState(1, 1)
	state.energyLayers[0][0] = 1
	state.coverageLayers[0][0] = 0.5
	state.ageHistory(1)
	wantEnergy := stereoPhosphorHistoryInjection
	wantCoverage := float32(0.5) * stereoPhosphorCoverageInjection
	if state.energyLayers[0][0] != 0 || math.Abs(float64(state.energyLayers[1][0]-wantEnergy)) > 0.000001 ||
		state.coverageLayers[0][0] != 0 || math.Abs(float64(state.coverageLayers[1][0]-wantCoverage)) > 0.000001 {
		t.Fatalf("first history step = energy %.6f coverage %.6f", state.energyLayers[1][0], state.coverageLayers[1][0])
	}
	if state.highlightEnergy[0] != 0.5 {
		t.Fatalf("fresh highlight injection = %.6f, want 0.5", state.highlightEnergy[0])
	}
	state.ageHistory(1)
	if math.Abs(float64(state.energyLayers[1][0]-wantEnergy*stereoPhosphorHistoryDecay)) > 0.000001 ||
		math.Abs(float64(state.coverageLayers[1][0]-wantCoverage*stereoPhosphorCoverageDecay)) > 0.000001 {
		t.Fatalf("second history step did not decay smoothly: energy %.6f coverage %.6f", state.energyLayers[1][0], state.coverageLayers[1][0])
	}
	if math.Abs(float64(state.highlightEnergy[0]-0.5*stereoPhosphorHighlightDecay)) > 0.000001 {
		t.Fatalf("fresh highlight did not decay smoothly: %.6f", state.highlightEnergy[0])
	}
	state.ageHistory(40)
	if state.energyLayers[1][0] != 0 || state.coverageLayers[1][0] != 0 || state.highlightEnergy[0] != 0 {
		t.Fatalf("sub-visible history was not cleared: energy %.6f coverage %.6f highlight %.6f", state.energyLayers[1][0], state.coverageLayers[1][0], state.highlightEnergy[0])
	}
}

func TestStereoPhosphorRepeatedFramesBuildPersistentCore(t *testing.T) {
	state := newStereoPhosphorTestState(1, 1)
	state.energyLayers[0][0] = 0.5
	state.ageHistory(1)
	onePass := state.energyLayers[1][0]
	state.energyLayers[0][0] = 0.5
	state.ageHistory(1)
	if state.energyLayers[1][0] <= onePass {
		t.Fatalf("repeated beam did not build phosphor intensity: first %.6f repeated %.6f", onePass, state.energyLayers[1][0])
	}
}

func TestStereoPhosphorAccumulatesAndClamps(t *testing.T) {
	state := newStereoPhosphorTestState(8, 8)
	state.splat(3, 3, 0.25)
	onePass := state.energyLayers[0][3*8+3]
	oneCoverage := state.coverageLayers[0][3*8+3]
	state.splat(3, 3, 0.25)
	if state.energyLayers[0][3*8+3] <= onePass {
		t.Fatal("repeated crossing did not become brighter")
	}
	if state.coverageLayers[0][3*8+3] != oneCoverage {
		t.Fatal("repeated crossing accumulated base coverage")
	}
	for i := 0; i < 20; i++ {
		state.splat(3, 3, 1)
	}
	if state.energyLayers[0][3*8+3] != stereoPhosphorLimit {
		t.Fatalf("intensity = %.3f, want clamp %.3f", state.energyLayers[0][3*8+3], stereoPhosphorLimit)
	}
}

func stereoPhosphorLayerSum(layer []float32) float32 {
	var total float32
	for _, value := range layer {
		total += value
	}
	return total
}

func stereoPhosphorLayerMax(layer []float32) float32 {
	var maximum float32
	for _, value := range layer {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func TestStereoPhosphorBeamUsesSqrtLengthCompensation(t *testing.T) {
	short := newStereoPhosphorTestState(128, 16)
	long := newStereoPhosphorTestState(128, 16)
	short.rasterSegment(stereoPhosphorPoint{X: 2, Y: 8}, stereoPhosphorPoint{X: 4, Y: 8}, 0.72)
	long.rasterSegment(stereoPhosphorPoint{X: 2, Y: 8}, stereoPhosphorPoint{X: 102, Y: 8}, 0.72)
	shortTotal := stereoPhosphorLayerSum(short.energyLayers[0])
	longTotal := stereoPhosphorLayerSum(long.energyLayers[0])
	wantRatio := float32(math.Sqrt(100.0 / 2.0))
	if math.Abs(float64(longTotal/shortTotal-wantRatio)) > 0.0001 {
		t.Fatalf("segment energy did not follow sqrt length: short %.6f long %.6f ratio %.6f", shortTotal, longTotal, longTotal/shortTotal)
	}
	if stereoPhosphorLayerMax(long.energyLayers[0]) >= stereoPhosphorLayerMax(short.energyLayers[0]) {
		t.Fatalf("long jump was not dimmer per pixel: short %.6f long %.6f", stereoPhosphorLayerMax(short.energyLayers[0]), stereoPhosphorLayerMax(long.energyLayers[0]))
	}
	if stereoPhosphorLayerMax(long.coverageLayers[0]) != stereoPhosphorLayerMax(short.coverageLayers[0]) {
		t.Fatal("long path lost its base coverage")
	}
	for x := 3; x <= 102; x++ {
		if long.coverageLayers[0][8*long.width+x] == 0 {
			t.Fatalf("long path coverage has a gap at x=%d", x)
		}
	}
}

func TestStereoPhosphorAgeEnergyIsMonotonic(t *testing.T) {
	previous := float32(-1)
	for i := 0; i < 16; i++ {
		value := stereoPhosphorAgeEnergy(i, 16)
		if value <= previous {
			t.Fatalf("age energy stopped increasing at %d: %.6f <= %.6f", i, value, previous)
		}
		previous = value
	}
	if math.Abs(float64(stereoPhosphorAgeEnergy(0, 16)-0.12)) > 0.000001 || math.Abs(float64(stereoPhosphorAgeEnergy(15, 16)-1.0)) > 0.000001 {
		t.Fatalf("unexpected age endpoints: %.6f %.6f", stereoPhosphorAgeEnergy(0, 16), stereoPhosphorAgeEnergy(15, 16))
	}
}

func TestStereoPhosphorArcBudgetKeepsNewestSuffix(t *testing.T) {
	quiet := make([]StereoPoint, 32)
	if start, trimmed := stereoPhosphorArcStart(quiet, 0, 0, 201, 201); start != 0 || trimmed {
		t.Fatalf("stationary path was trimmed: %d, %v", start, trimmed)
	}
	busy := make([]StereoPoint, 32)
	for i := range busy {
		if i%2 == 0 {
			busy[i].X = -0.8
		} else {
			busy[i].X = 0.8
		}
	}
	start, trimmed := stereoPhosphorArcStart(busy, 0, 0, 201, 201)
	if !trimmed || start <= 0 || start > len(busy)-2 {
		t.Fatalf("busy path suffix = %d, %v", start, trimmed)
	}
}

func TestStereoPhosphorSoftKneeDoesNotPileUpAtOneRadius(t *testing.T) {
	const size = 201
	center := float32(size-1) * 0.5
	medium := stereoPhosphorMapPoint(StereoPoint{Y: 0.62}, 0, size, size)
	loud := stereoPhosphorMapPoint(StereoPoint{Y: 2.0}, 0, size, size)
	mediumRadius := float32(math.Hypot(float64(medium.X-center), float64(medium.Y-center)))
	loudRadius := float32(math.Hypot(float64(loud.X-center), float64(loud.Y-center)))
	if mediumRadius >= loudRadius || loudRadius >= 96.01 || loudRadius-mediumRadius < 0.5 {
		t.Fatalf("soft-knee radii = medium %.3f, loud %.3f", mediumRadius, loudRadius)
	}
}

func TestStereoPhosphorMappingPreservesDirections(t *testing.T) {
	const size = 201
	center := float32(size-1) * 0.5
	mono := stereoPhosphorMapPoint(StereoPoint{Y: 0.4}, 0, size, size)
	antiphase := stereoPhosphorMapPoint(StereoPoint{X: 0.4}, 0, size, size)
	left := stereoPhosphorMapPoint(StereoPoint{X: 0.3, Y: 0.3}, 0, size, size)
	right := stereoPhosphorMapPoint(StereoPoint{X: -0.3, Y: 0.3}, 0, size, size)
	if math.Abs(float64(mono.X-center)) > 0.001 || mono.Y >= center {
		t.Fatalf("mono direction = %+v", mono)
	}
	if antiphase.X >= center || math.Abs(float64(antiphase.Y-center)) > 0.001 {
		t.Fatalf("antiphase direction = %+v", antiphase)
	}
	if left.X >= center || right.X <= center || left.Y >= center || right.Y >= center {
		t.Fatalf("left/right directions = left:%+v right:%+v", left, right)
	}
}

func TestStereoPhosphorLineScaleMatchesDotAndRay(t *testing.T) {
	if stereoLineDisplayGain != 1.35 || stereoDisplayGain != stereoLineDisplayGain {
		t.Fatalf("shared display gain = phosphor %.2f direct %.2f, want 1.35", stereoLineDisplayGain, stereoDisplayGain)
	}

	const size = 1041
	center := float32(size-1) * 0.5
	point := stereoPhosphorMapPoint(StereoPoint{Y: 0.25}, 0, size, size)
	radiusFraction := (center - point.Y) / center
	want := float32(0.25*0.82) * stereoLineDisplayGain
	if math.Abs(float64(radiusFraction-want)) > 0.001 {
		t.Fatalf("LINE radius = %.4f, want %.4f", radiusFraction, want)
	}
	fullScale := stereoPhosphorMapPoint(StereoPoint{Y: 1}, 0, size, size)
	fullScaleFraction := (center - fullScale.Y) / center
	if fullScaleFraction < 0.95 || fullScaleFraction > 0.96 {
		t.Fatalf("soft-limited LINE full-scale radius = %.4f, want 0.95..0.96", fullScaleFraction)
	}
}

func TestStereoPhosphorPolarMappingPreservesDirectionsAndSoftLimit(t *testing.T) {
	const size = 201
	center := float32(size-1) * 0.5
	mono := stereoPhosphorMapPoint(StereoPoint{Y: 0.4}, 2, size, size)
	left := stereoPhosphorMapPoint(StereoPoint{X: 0.3, Y: 0.3}, 2, size, size)
	right := stereoPhosphorMapPoint(StereoPoint{X: -0.3, Y: 0.3}, 2, size, size)
	if math.Abs(float64(mono.X-center)) > 0.001 || mono.Y >= center {
		t.Fatalf("polar mono direction = %+v", mono)
	}
	if left.X >= center || right.X <= center {
		t.Fatalf("polar left/right directions = left:%+v right:%+v", left, right)
	}
	fullScale := stereoPhosphorMapPoint(StereoPoint{Y: 1}, 2, size, size)
	radiusFraction := (center - fullScale.Y) / center
	if radiusFraction < 0.95 || radiusFraction > 0.96 {
		t.Fatalf("polar soft-limited radius = %.4f, want 0.95..0.96", radiusFraction)
	}
}

func TestStereoPhosphorStyleInitialWindowsPreserveCurrentRanges(t *testing.T) {
	for _, test := range []struct {
		name               string
		count, limit, want int
	}{
		{name: "DOT", count: 960, limit: stereoPhosphorDotInitialMax, want: 60},
		{name: "RAY", count: 960, limit: stereoPhosphorRayInitialMax, want: 480},
	} {
		state := stereoPhosphorState{}
		start, discontinuity := state.newPointStartLimited(test.count, uint64(test.count), test.limit)
		if start != test.want || !discontinuity {
			t.Fatalf("%s initial start = %d, %v; want %d, true", test.name, start, discontinuity, test.want)
		}
	}
}

func TestStereoPhosphorDotSplatDoesNotConnectSamples(t *testing.T) {
	state := newStereoPhosphorTestState(41, 21)
	state.splatFresh(stereoPhosphorPoint{X: 10, Y: 10}, 0.5, 1)
	state.splatFresh(stereoPhosphorPoint{X: 30, Y: 10}, 0.5, 1)
	coverage := state.coverageLayers[stereoPhosphorCurrentLayer]
	if coverage[10*state.width+10] == 0 || coverage[10*state.width+30] == 0 {
		t.Fatal("DOT endpoints were not rasterized")
	}
	if coverage[10*state.width+20] != 0 {
		t.Fatal("DOT samples were joined by unintended coverage")
	}
}

func TestStereoPhosphorDotUsesLargerParticleCoverage(t *testing.T) {
	state := newStereoPhosphorTestState(21, 21)
	state.splatFresh(stereoPhosphorPoint{X: 10, Y: 10}, 1, 1)
	coverage := state.coverageLayers[stereoPhosphorCurrentLayer]
	if coverage[10*state.width+12] == 0 {
		t.Fatal("DOT particle did not reach two pixels from its center")
	}
	if coverage[10*state.width+13] != 0 {
		t.Fatal("DOT particle exceeded its bounded radius")
	}
}

func TestStereoPhosphorDotHistoryOutlastsLineHistory(t *testing.T) {
	lineState := newStereoPhosphorTestState(1, 1)
	dotState := newStereoPhosphorTestState(1, 1)
	dotState.renderStyle = 1
	lineState.energyLayers[0][0], dotState.energyLayers[0][0] = 0.5, 0.5
	lineState.coverageLayers[0][0], dotState.coverageLayers[0][0] = 1, 1
	lineState.ageHistory(1)
	dotState.ageHistory(1)
	if dotState.energyLayers[1][0] <= lineState.energyLayers[1][0] ||
		dotState.coverageLayers[1][0] <= lineState.coverageLayers[1][0] {
		t.Fatalf("DOT history did not outlast LINE: energy %.3f/%.3f coverage %.3f/%.3f",
			dotState.energyLayers[1][0], lineState.energyLayers[1][0],
			dotState.coverageLayers[1][0], lineState.coverageLayers[1][0])
	}
}

func TestStereoPhosphorDotParticlesAreMultiScaleStableAndBounded(t *testing.T) {
	var tiers [3]int
	for sequence := uint64(1); sequence <= 4096; sequence++ {
		offset, radius, energy := stereoPhosphorDotParticle(sequence, 450, 450, 1, false)
		repeatedOffset, repeatedRadius, repeatedEnergy := stereoPhosphorDotParticle(sequence, 450, 450, 1, false)
		if offset != repeatedOffset || radius != repeatedRadius || energy != repeatedEnergy {
			t.Fatalf("DOT particle %d was not deterministic", sequence)
		}
		distance := float32(math.Hypot(float64(offset.X), float64(offset.Y)))
		switch {
		case distance <= 5.001:
			tiers[0]++
		case distance <= 12.001:
			tiers[1]++
		case distance <= 18.001:
			tiers[2]++
		default:
			t.Fatalf("DOT particle %d scatter radius = %.3f", sequence, distance)
		}
		if radius < stereoPhosphorDotMinRadius || radius > stereoPhosphorDotMaxRadius {
			t.Fatalf("DOT particle %d radius = %.3f", sequence, radius)
		}
		if energy < 0.90 || energy > 1.25 {
			t.Fatalf("DOT particle %d energy = %.3f", sequence, energy)
		}
		haloOffset, haloRadius, haloEnergy := stereoPhosphorDotParticle(sequence, 450, 450, 16, true)
		haloDistance := float32(math.Hypot(float64(haloOffset.X), float64(haloOffset.Y)))
		if haloDistance < 9.999 || haloDistance > 22.001 || haloRadius < stereoPhosphorDotMinRadius || haloRadius > 1.951 || haloEnergy < 0.35 || haloEnergy > 0.65 {
			t.Fatalf("DOT halo %d = distance %.3f radius %.3f energy %.3f", sequence, haloDistance, haloRadius, haloEnergy)
		}
	}
	if tiers[0] < 700 || tiers[0] > 950 || tiers[1] < 2250 || tiers[1] > 2650 || tiers[2] < 700 || tiers[2] > 950 {
		t.Fatalf("DOT scatter tiers = %v, want approximately 20/60/20 percent", tiers)
	}
}

func TestStereoPhosphorDotDensityRaisesBrightnessWithoutSaturation(t *testing.T) {
	_, _, sparse := stereoPhosphorDotParticle(99, 450, 450, 1, false)
	_, _, dense := stereoPhosphorDotParticle(99, 450, 450, 64, false)
	if dense <= sparse || dense > 1.25 {
		t.Fatalf("DOT density energy = sparse %.3f dense %.3f", sparse, dense)
	}
}

func TestStereoPhosphorDotGridAndParticleBudgetScaleWithCanvas(t *testing.T) {
	if cell := stereoPhosphorDotCellSize(450, 450); cell != 8 {
		t.Fatalf("450 px DOT cell = %d, want 8", cell)
	}
	if limit := stereoPhosphorDotParticleLimit(450, 450); limit != 145 {
		t.Fatalf("450 px DOT particle limit = %d, want 145", limit)
	}
	if stereoPhosphorDotCellSize(100, 100) != 6 || stereoPhosphorDotCellSize(1000, 1000) != 10 {
		t.Fatal("DOT cell size did not respect 6..10 px bounds")
	}
	if stereoPhosphorDotParticleLimit(100, 100) != 80 || stereoPhosphorDotParticleLimit(1000, 1000) != 180 {
		t.Fatal("DOT particle budget did not respect 80..180 bounds")
	}
}

func TestStereoPhosphorDotFlushIsBoundedAndClearsDensityState(t *testing.T) {
	state := newStereoPhosphorTestState(450, 450)
	sequence := uint64(0)
	for y := state.dotCellSize / 2; y < state.height; y += state.dotCellSize {
		for x := state.dotCellSize / 2; x < state.width; x += state.dotCellSize {
			sequence++
			state.addDotPoint(stereoPhosphorPoint{X: float32(x), Y: float32(y)}, 1, sequence, 7)
		}
	}
	emitted := state.flushDotBins()
	if emitted <= 0 || emitted > stereoPhosphorDotParticleLimit(state.width, state.height) {
		t.Fatalf("DOT emitted %d particles", emitted)
	}
	if len(state.dotActive) != 0 || state.dotFrameValid {
		t.Fatal("DOT density state survived its frame flush")
	}
	for _, bin := range state.dotBins {
		if bin.Hits != 0 {
			t.Fatal("DOT density bin survived its frame flush")
		}
	}
}

func TestStereoPhosphorDotDensityIsIndependentOfSampleOrder(t *testing.T) {
	type sample struct {
		point    stereoPhosphorPoint
		sequence uint64
	}
	samples := make([]sample, 0, 96)
	for i := 0; i < 96; i++ {
		samples = append(samples, sample{
			point:    stereoPhosphorPoint{X: float32(80 + (i*17)%260), Y: float32(90 + (i*29)%240)},
			sequence: uint64(i + 1),
		})
	}
	forward := newStereoPhosphorTestState(450, 450)
	reverse := newStereoPhosphorTestState(450, 450)
	for _, sample := range samples {
		forward.addDotPoint(sample.point, 1, sample.sequence, 3)
	}
	for i := len(samples) - 1; i >= 0; i-- {
		reverse.addDotPoint(samples[i].point, 1, samples[i].sequence, 3)
	}
	if forward.flushDotBins() != reverse.flushDotBins() {
		t.Fatal("DOT sample order changed the particle count")
	}
	for i := range forward.energyLayers[0] {
		if math.Abs(float64(forward.energyLayers[0][i]-reverse.energyLayers[0][i])) > 0.000001 ||
			math.Abs(float64(forward.coverageLayers[0][i]-reverse.coverageLayers[0][i])) > 0.000001 {
			t.Fatalf("DOT sample order changed pixel %d", i)
		}
	}
}

func TestStereoPhosphorDotDensityBreaksContinuousSampleTrack(t *testing.T) {
	state := newStereoPhosphorTestState(450, 450)
	sequence := uint64(0)
	for x := 50; x <= 400; x++ {
		sequence++
		state.addDotPoint(stereoPhosphorPoint{X: float32(x), Y: 225}, 1, sequence, 11)
	}
	emitted := state.flushDotBins()
	if emitted < 40 {
		t.Fatalf("DOT line produced only %d cloud particles", emitted)
	}
	coverage := state.coverageLayers[stereoPhosphorCurrentLayer]
	longest, run := 0, 0
	spreadPixels := 0
	for x := 40; x <= 410; x++ {
		if coverage[225*state.width+x] > 0 {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
		for y := 205; y <= 245; y++ {
			if absInt(y-225) >= 5 && coverage[y*state.width+x] > 0 {
				spreadPixels++
			}
		}
	}
	if longest > 12 {
		t.Fatalf("DOT retained a %d px continuous centerline", longest)
	}
	if spreadPixels < 80 {
		t.Fatalf("DOT cloud spread covered only %d off-track pixels", spreadPixels)
	}
}

func TestStereoPhosphorDotResetClearsPendingDensity(t *testing.T) {
	state := newStereoPhosphorTestState(101, 101)
	state.addDotPoint(stereoPhosphorPoint{X: 50, Y: 50}, 1, 1, 0)
	if len(state.dotActive) != 1 || !state.dotFrameValid {
		t.Fatal("DOT test did not create pending density")
	}
	state.reset()
	if len(state.dotActive) != 0 || state.dotFrameValid {
		t.Fatal("DOT reset retained pending density")
	}
	for _, bin := range state.dotBins {
		if bin.Hits != 0 {
			t.Fatal("DOT reset retained a populated density bin")
		}
	}
}

func TestStereoPhosphorDotCoalescedTickCommitsBothDensityFrames(t *testing.T) {
	state := newStereoPhosphorTestState(101, 101)
	state.dc, state.bitmap = 1, 1
	first := []StereoPoint{{X: 0, Y: 0.2}, {X: 0.1, Y: 0.25}}
	if _, composed := state.advanceStyle(first, uint64(len(first)), 1, 0, 1, 101, 101); composed {
		t.Fatal("first odd DOT tick composed unexpectedly")
	}
	second := []StereoPoint{{X: -0.1, Y: 0.3}, {X: 0, Y: 0.35}}
	if _, composed := state.advanceStyle(second, uint64(len(first)+len(second)), 3, 0, 1, 101, 101); !composed {
		t.Fatal("coalesced DOT tick did not compose")
	}
	if state.dotFrameValid || len(state.dotActive) != 0 {
		t.Fatal("coalesced DOT compose left density pending")
	}
	currentEnergy, historyEnergy := float32(0), float32(0)
	for i := range state.energyLayers[0] {
		currentEnergy += state.energyLayers[stereoPhosphorCurrentLayer][i]
		historyEnergy += state.energyLayers[stereoPhosphorHistoryLayer][i]
	}
	if currentEnergy <= 0 || historyEnergy <= 0 {
		t.Fatalf("coalesced DOT frames = current %.3f history %.3f", currentEnergy, historyEnergy)
	}
}

func TestStereoPhosphorPointSequenceTracksAbsoluteSamples(t *testing.T) {
	if got := stereoPhosphorPointSequence(960, 960, 60); got != 61 {
		t.Fatalf("first retained sequence = %d, want 61", got)
	}
	if got := stereoPhosphorPointSequence(1200, 240, 239); got != 1200 {
		t.Fatalf("newest sequence = %d, want 1200", got)
	}
}

func TestStereoPhosphorRayBinsPreferFarthestEndpointAndProtectCenter(t *testing.T) {
	state := newStereoPhosphorTestState(101, 101)
	center := stereoPhosphorCenter(state.width, state.height)
	for i := 0; i < 12; i++ {
		state.addRayPoint(stereoPhosphorPoint{X: 60, Y: 50}, center, 0.5)
	}
	state.addRayPoint(stereoPhosphorPoint{X: 90, Y: 50}, center, 1)
	if !state.flushRayBins(center) {
		t.Fatal("RAY bins did not produce a ray")
	}
	coverage := state.coverageLayers[stereoPhosphorCurrentLayer]
	if coverage[50*state.width+50] != 0 {
		t.Fatalf("RAY center coverage = %.3f, want 0", coverage[50*state.width+50])
	}
	if coverage[50*state.width+90] == 0 {
		t.Fatal("RAY did not retain the farthest endpoint")
	}
	if state.energyLayers[stereoPhosphorCurrentLayer][50*state.width+90] <=
		state.energyLayers[stereoPhosphorCurrentLayer][50*state.width+53] {
		t.Fatal("RAY energy did not increase toward its endpoint")
	}
	for _, bin := range state.rayBins {
		if bin.Hits != 0 {
			t.Fatal("RAY bins were not cleared after rasterization")
		}
	}
}

func TestStereoPhosphorCurveStaysInBounds(t *testing.T) {
	state := newStereoPhosphorTestState(16, 16)
	for _, point := range []stereoPhosphorPoint{{-4, -4}, {0, 0}, {8, 15}, {15, 8}, {20, 20}} {
		state.addRawPoint(stereoPhosphorVertex{Position: point, Energy: 0.5}, 2.5)
	}
	for i, value := range state.energyLayers[0] {
		if value < 0 || value > stereoPhosphorLimit {
			t.Fatalf("pixel %d out of range: %.3f", i, value)
		}
	}
}

func TestStereoPhosphorToneMappingSeparatesCurrentAndHistory(t *testing.T) {
	trail := rgb(38, 88, 105)
	lineColor := rgb(60, 153, 181)
	core := rgb(116, 218, 238)
	peak := rgb(210, 248, 255)
	if color := stereoPhosphorCoreColor(trail, lineColor, core, peak, 0); color != trail {
		t.Fatalf("history color = %#x, want trail %#x", color, trail)
	}
	if color := stereoPhosphorCoreColor(trail, lineColor, core, peak, 1.5); color != peak {
		t.Fatalf("peak color = %#x, want %#x", color, peak)
	}
	if stereoPhosphorCoreAlpha(0.7) <= stereoPhosphorCoreAlpha(0.05) {
		t.Fatal("strong current did not receive higher alpha")
	}
	if stereoPhosphorGlowAlpha(10) > 0.320001 {
		t.Fatalf("glow exceeded cap: %.6f", stereoPhosphorGlowAlpha(10))
	}
	if stereoPhosphorFreshAlpha(1) != stereoPhosphorHighlightAlpha || stereoPhosphorFreshAlpha(0.5) != stereoPhosphorHighlightAlpha*0.5 {
		t.Fatalf("fresh highlight alpha response = %.6f %.6f", stereoPhosphorFreshAlpha(1), stereoPhosphorFreshAlpha(0.5))
	}
}

func TestStereoPhosphorGlowRadiusScalesWithoutChangingBlurComplexity(t *testing.T) {
	if got := stereoPhosphorGlowRadius(100, 100); got != stereoPhosphorMinGlowRadius {
		t.Fatalf("small glow radius = %d", got)
	}
	if got := stereoPhosphorGlowRadius(400, 400); got != 20 {
		t.Fatalf("medium glow radius = %d", got)
	}
	if got := stereoPhosphorGlowRadius(1000, 1000); got != stereoPhosphorMaxGlowRadius {
		t.Fatalf("large glow radius = %d", got)
	}
}

func TestStereoPhosphorRenderIsPremultipliedAndGlows(t *testing.T) {
	state := newStereoPhosphorTestState(21, 21)
	state.energyLayers[0][10*21+10] = 1
	state.coverageLayers[0][10*21+10] = 1
	state.renderPixels(rgb(38, 88, 105), rgb(60, 153, 181), rgb(116, 218, 238), rgb(210, 248, 255))
	center := (10*21 + 10) * 4
	neighbor := (10*21 + 14) * 4
	if state.pixels[center+3] == 0 || state.pixels[neighbor+3] == 0 {
		t.Fatalf("core or glow missing: center=%d neighbor=%d", state.pixels[center+3], state.pixels[neighbor+3])
	}
	for i := 0; i < len(state.pixels); i += 4 {
		alpha := state.pixels[i+3]
		if state.pixels[i] > alpha || state.pixels[i+1] > alpha || state.pixels[i+2] > alpha {
			t.Fatalf("pixel %d is not premultiplied: BGRA=%v", i/4, state.pixels[i:i+4])
		}
	}
}

func TestStereoPhosphorParallelRenderMatchesSequential(t *testing.T) {
	sequential := newStereoPhosphorTestState(320, 240)
	parallel := newStereoPhosphorTestState(320, 240)
	for i := 0; i < 240; i++ {
		angle := float64(i) * math.Pi / 120
		x := float32(160 + math.Cos(angle)*70)
		y := float32(120 + math.Sin(angle)*85)
		sequential.splat(x, y, 0.55)
		parallel.splat(x, y, 0.55)
	}
	colors := [4]uint32{rgb(38, 88, 105), rgb(60, 153, 181), rgb(116, 218, 238), rgb(236, 253, 255)}
	sequential.renderPixels(colors[0], colors[1], colors[2], colors[3])
	parallel.dc = 1 // Enable workers without requiring a Win32 DIB in this test.
	parallel.renderPixels(colors[0], colors[1], colors[2], colors[3])
	parallel.stopRenderPool()
	parallel.dc = 0
	for i := range sequential.pixels {
		if sequential.pixels[i] != parallel.pixels[i] {
			t.Fatalf("parallel pixel %d = %d, sequential = %d", i, parallel.pixels[i], sequential.pixels[i])
		}
	}
}

func TestStereoPhosphorResponseLUTPreservesVisualResponse(t *testing.T) {
	state := newStereoPhosphorTestState(1, 1)
	colors := [4]uint32{rgb(38, 88, 105), rgb(60, 153, 181), rgb(116, 218, 238), rgb(236, 253, 255)}
	state.ensureColorLUT(colors[0], colors[1], colors[2], colors[3])
	channelDelta := func(a, b uint32, shift uint) int {
		delta := int(byte(a>>shift)) - int(byte(b>>shift))
		if delta < 0 {
			return -delta
		}
		return delta
	}
	for sample := 0; sample <= 1000; sample++ {
		value := float32(sample) * stereoPhosphorLimit / 1000
		index := stereoPhosphorResponseIndex(value, stereoPhosphorLimit)
		if delta := math.Abs(float64(stereoPhosphorResponses.core[index] - stereoPhosphorCoreAlpha(value))); delta > 0.001 {
			t.Fatalf("core LUT error at %.4f = %.6f", value, delta)
		}
		if delta := math.Abs(float64(stereoPhosphorResponses.glow[index] - stereoPhosphorGlowAlpha(value))); delta > 0.001 {
			t.Fatalf("glow LUT error at %.4f = %.6f", value, delta)
		}
		directCore := stereoPhosphorCoreColor(colors[0], colors[1], colors[2], colors[3], value)
		cachedCore := state.coreColorLUT[index]
		for _, shift := range []uint{0, 8, 16} {
			if delta := channelDelta(directCore, cachedCore, shift); delta > 1 {
				t.Fatalf("core color LUT channel error at %.4f = %d", value, delta)
			}
		}
		freshness := float32(sample) / 1000
		directFresh := lerpColor(colors[2], colors[3], freshness)
		cachedFresh := state.freshColorLUT[stereoPhosphorResponseIndex(freshness, 1)]
		for _, shift := range []uint{0, 8, 16} {
			if delta := channelDelta(directFresh, cachedFresh, shift); delta > 1 {
				t.Fatalf("fresh color LUT channel error at %.4f = %d", freshness, delta)
			}
		}
	}
}

func TestStereoPhosphorCoverageProvidesVisibleFreshHighlight(t *testing.T) {
	alphaForLayer := func(layer int, coverage float32) byte {
		state := newStereoPhosphorTestState(3, 3)
		state.coverageLayers[layer][4] = coverage
		state.renderPixels(rgb(38, 88, 105), rgb(60, 153, 181), rgb(116, 218, 238), rgb(210, 248, 255))
		return state.pixels[4*4+3]
	}
	current := alphaForLayer(stereoPhosphorCurrentLayer, 1)
	history := alphaForLayer(stereoPhosphorHistoryLayer, stereoPhosphorCoverageInjection)
	if current < 188 || current > 191 {
		t.Fatalf("current highlighted alpha = %d, want about 74%%", current)
	}
	if !(current > history && history > 0) {
		t.Fatalf("base alpha ages are not separated: current=%d history=%d", current, history)
	}
}

func TestStereoPhosphorFreshHighlightHasFeatheredThickness(t *testing.T) {
	state := newStereoPhosphorTestState(5, 5)
	state.splat(2, 2, 0)
	state.renderPixels(rgb(38, 88, 105), rgb(60, 153, 181), rgb(116, 218, 238), rgb(236, 253, 255))
	alphaAt := func(x, y int) byte { return state.pixels[(y*5+x)*4+3] }
	center := alphaAt(2, 2)
	edge := alphaAt(2, 1)
	corner := alphaAt(1, 1)
	outside := alphaAt(0, 0)
	if !(center > edge && edge > corner && corner > outside) {
		t.Fatalf("feathered alpha = center:%d edge:%d corner:%d outside:%d", center, edge, corner, outside)
	}
}

func TestStereoPhosphorStrokeCoverageHasNoCenterlineHoles(t *testing.T) {
	state := newStereoPhosphorTestState(72, 24)
	state.rasterSegment(
		stereoPhosphorPoint{X: 3.2, Y: 10.35},
		stereoPhosphorPoint{X: 68.4, Y: 10.35},
		0.5,
	)
	for x := 4; x <= 68; x++ {
		coverage := state.coverageLayers[stereoPhosphorCurrentLayer][10*state.width+x]
		if coverage < 0.75 {
			t.Fatalf("centerline coverage at x=%d is %.3f", x, coverage)
		}
	}
}

func TestStereoPhosphorStrokeCoverageTracksFreshness(t *testing.T) {
	state := newStereoPhosphorTestState(48, 24)
	state.rasterStrokeSegment(
		stereoPhosphorPoint{X: 3, Y: 8},
		stereoPhosphorPoint{X: 44, Y: 8},
		0.25, 0.25,
	)
	dim := state.coverageLayers[stereoPhosphorCurrentLayer][8*state.width+24]
	clear(state.coverageLayers[stereoPhosphorCurrentLayer])
	state.rasterStrokeSegment(
		stereoPhosphorPoint{X: 3, Y: 8},
		stereoPhosphorPoint{X: 44, Y: 8},
		1, 1,
	)
	bright := state.coverageLayers[stereoPhosphorCurrentLayer][8*state.width+24]
	if math.Abs(float64(dim-0.25)) > 0.0001 || math.Abs(float64(bright-1)) > 0.0001 {
		t.Fatalf("freshness coverage = dim %.3f bright %.3f", dim, bright)
	}
}

func TestStereoPhosphorCurveInterpolatesFreshness(t *testing.T) {
	state := newStereoPhosphorTestState(64, 24)
	vertices := []stereoPhosphorVertex{
		{Position: stereoPhosphorPoint{X: 4, Y: 12}, Energy: 0.5, Freshness: 0.1},
		{Position: stereoPhosphorPoint{X: 20, Y: 12}, Energy: 0.5, Freshness: 0.2},
		{Position: stereoPhosphorPoint{X: 40, Y: 12}, Energy: 0.5, Freshness: 1.0},
		{Position: stereoPhosphorPoint{X: 58, Y: 12}, Energy: 0.5, Freshness: 1.0},
	}
	state.rasterCatmullRom(vertices[0], vertices[1], vertices[2], vertices[3])
	left := state.coverageLayers[stereoPhosphorCurrentLayer][12*state.width+21]
	right := state.coverageLayers[stereoPhosphorCurrentLayer][12*state.width+39]
	if !(right > left*2) {
		t.Fatalf("curve freshness did not rise along the segment: left %.3f right %.3f", left, right)
	}
}

func TestStereoPhosphorCentripetalTangentsRemainStableWithUnevenSpacing(t *testing.T) {
	p0 := stereoPhosphorPoint{X: 0, Y: 0}
	p1 := stereoPhosphorPoint{X: 1, Y: 0}
	p2 := stereoPhosphorPoint{X: 18, Y: 8}
	p3 := stereoPhosphorPoint{X: 19, Y: 9}
	m1, m2 := stereoPhosphorCentripetalTangents(p0, p1, p2, p3)
	segmentLength := stereoPhosphorDistance(p1, p2)
	for index, tangent := range []stereoPhosphorPoint{m1, m2} {
		length := stereoPhosphorDistance(stereoPhosphorPoint{}, tangent)
		if math.IsNaN(float64(length)) || math.IsInf(float64(length), 0) || length > segmentLength*1.5 {
			t.Fatalf("unstable centripetal tangent %d: %+v length %.3f", index, tangent, length)
		}
	}
}

func TestStereoPhosphorAdaptiveFlatteningUsesCurvature(t *testing.T) {
	straight := stereoPhosphorCurveSamples{}
	straight.values[0] = stereoPhosphorCurveSample{Position: stereoPhosphorPoint{X: 0, Y: 0}}
	straight.count = 1
	stereoPhosphorFlattenCubic(&straight,
		stereoPhosphorPoint{X: 0, Y: 0},
		stereoPhosphorPoint{X: 3, Y: 0},
		stereoPhosphorPoint{X: 7, Y: 0},
		stereoPhosphorPoint{X: 10, Y: 0},
		0, 1, 0,
	)
	curved := stereoPhosphorCurveSamples{}
	curved.values[0] = stereoPhosphorCurveSample{Position: stereoPhosphorPoint{X: 0, Y: 0}}
	curved.count = 1
	stereoPhosphorFlattenCubic(&curved,
		stereoPhosphorPoint{X: 0, Y: 0},
		stereoPhosphorPoint{X: 0, Y: 12},
		stereoPhosphorPoint{X: 10, Y: 12},
		stereoPhosphorPoint{X: 10, Y: 0},
		0, 1, 0,
	)
	if straight.count != 2 || curved.count <= straight.count || curved.count > len(curved.values) {
		t.Fatalf("adaptive sample counts = straight %d curved %d capacity %d", straight.count, curved.count, len(curved.values))
	}
}

func TestStereoPhosphorSmoothCurveCarriesAcrossBatches(t *testing.T) {
	state := newStereoPhosphorTestState(96, 96)
	for i := 0; i < 18; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position: stereoPhosphorPoint{X: 8 + float32(i)*2, Y: 48 + float32(math.Sin(float64(i)*0.2))*12},
			Energy:   0.5,
		}, 2.5)
	}
	before := state.emittedControls
	beforeSmoothCount := state.smoothCount
	for i := 18; i < 36; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position: stereoPhosphorPoint{X: 8 + float32(i)*2, Y: 48 + float32(math.Sin(float64(i)*0.2))*12},
			Energy:   0.5,
		}, 2.5)
	}
	if beforeSmoothCount != 3 || state.smoothCount != 3 || state.emittedControls <= before {
		t.Fatalf("curve state did not carry across batches: smooth %d->%d controls %d->%d", beforeSmoothCount, state.smoothCount, before, state.emittedControls)
	}
}

func TestStereoPhosphorResetClearsHistory(t *testing.T) {
	state := stereoPhosphorState{
		energyLayers:      [2][]float32{{0.5}, {0.2}},
		coverageLayers:    [2][]float32{{1}, {0.5}},
		highlightEnergy:   []float32{0.7},
		compositeEnergy:   []float32{0.3},
		compositeCoverage: []float32{0.4},
		blurScratch:       []float32{0.2},
		glowEnergy:        []float32{0.1},
		pixels:            []byte{1, 2, 3, 4},
		sequenceValid:     true,
		tickValid:         true,
		rawCount:          2,
		smoothCount:       2,
		curveValid:        true,
		tailFlushed:       true,
	}
	state.reset()
	if state.energyLayers[0][0] != 0 || state.energyLayers[1][0] != 0 || state.coverageLayers[0][0] != 0 || state.coverageLayers[1][0] != 0 || state.highlightEnergy[0] != 0 || state.compositeEnergy[0] != 0 || state.compositeCoverage[0] != 0 || state.blurScratch[0] != 0 || state.glowEnergy[0] != 0 || state.pixels[3] != 0 || state.sequenceValid || state.tickValid || state.displayFrameValid || state.composedFrameValid || state.rawCount != 0 || state.filteredCount != 0 || state.resampleValid || state.resampleDistance != 0 || state.resampleEnergy != 0 || state.smoothCount != 0 || state.curveValid || state.tailFlushed {
		t.Fatalf("reset left phosphor history behind: %+v", state)
	}
}

func TestStereoPhosphorSevenPointFilterPreservesStraightGeometry(t *testing.T) {
	constant := [7]stereoPhosphorVertex{}
	for i := range constant {
		constant[i] = stereoPhosphorVertex{
			Position: stereoPhosphorPoint{X: 12, Y: 30},
			Energy:   float32(i),
		}
	}
	filtered := stereoPhosphorFilteredPoint(constant)
	if filtered.Position.X != 12 || filtered.Position.Y != 30 || filtered.Energy != constant[3].Energy {
		t.Fatalf("constant filter output = %+v", filtered)
	}

	line := [7]stereoPhosphorVertex{}
	for i := range line {
		line[i].Position = stereoPhosphorPoint{X: 40, Y: float32(i * 10)}
	}
	filtered = stereoPhosphorFilteredPoint(line)
	if filtered.Position.X != 40 || math.Abs(float64(filtered.Position.Y-30)) > 0.0001 {
		t.Fatalf("straight filter output = %+v", filtered.Position)
	}

	impulse := [7]stereoPhosphorVertex{}
	impulse[3].Position.Y = 1
	filtered = stereoPhosphorFilteredPoint(impulse)
	if math.Abs(float64(filtered.Position.Y-20.0/64.0)) > 0.0001 {
		t.Fatalf("seven-point impulse response = %.6f", filtered.Position.Y)
	}
}

func TestStereoPhosphorControlSpacingUsesPhysicalBounds(t *testing.T) {
	if got := stereoPhosphorControlSpacing(101, 101); got != stereoPhosphorMinSpacing {
		t.Fatalf("small spacing = %.3f", got)
	}
	if got := stereoPhosphorControlSpacing(401, 401); math.Abs(float64(got-3.2)) > 0.0001 {
		t.Fatalf("normal spacing = %.3f", got)
	}
	if got := stereoPhosphorControlSpacing(1001, 1001); got != stereoPhosphorMaxSpacing {
		t.Fatalf("large spacing = %.3f", got)
	}
}

func TestStereoPhosphorAdaptiveSpacingDensifiesTurns(t *testing.T) {
	const base = float32(3.2)
	straight := stereoPhosphorAdaptiveSpacing(base,
		stereoPhosphorPoint{X: 0, Y: 0},
		stereoPhosphorPoint{X: 5, Y: 0},
		stereoPhosphorPoint{X: 10, Y: 0},
	)
	rightAngle := stereoPhosphorAdaptiveSpacing(base,
		stereoPhosphorPoint{X: 0, Y: 0},
		stereoPhosphorPoint{X: 5, Y: 0},
		stereoPhosphorPoint{X: 5, Y: 5},
	)
	reversal := stereoPhosphorAdaptiveSpacing(base,
		stereoPhosphorPoint{X: 0, Y: 0},
		stereoPhosphorPoint{X: 5, Y: 0},
		stereoPhosphorPoint{X: 0, Y: 0},
	)
	if straight != base || !(rightAngle < straight) || !(reversal < rightAngle) {
		t.Fatalf("adaptive spacing = straight %.3f right %.3f reverse %.3f", straight, rightAngle, reversal)
	}
	if reversal < stereoPhosphorCurveMinSpacing {
		t.Fatalf("reverse spacing %.3f fell below floor %.3f", reversal, stereoPhosphorCurveMinSpacing)
	}
}

func TestStereoPhosphorResamplingReducesDenseControlPoints(t *testing.T) {
	state := newStereoPhosphorTestState(128, 128)
	const inputCount = 200
	for i := 0; i < inputCount; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position: stereoPhosphorPoint{
				X: 64 + float32(math.Sin(float64(i)*0.7))*0.35,
				Y: 10 + float32(i)*0.35,
			},
			Energy: 0.5,
		}, 2.5)
	}
	if state.emittedControls >= inputCount/2 {
		t.Fatalf("dense path emitted %d controls for %d inputs", state.emittedControls, inputCount)
	}
	if state.emittedControls < 20 {
		t.Fatalf("dense path was over-simplified: %d controls", state.emittedControls)
	}
}

func TestStereoPhosphorResamplerCarriesAcrossBatchesAndResetBreaksIt(t *testing.T) {
	state := newStereoPhosphorTestState(64, 64)
	for i := 0; i < 12; i++ {
		state.addRawPoint(stereoPhosphorVertex{Position: stereoPhosphorPoint{X: float32(i), Y: 20}, Energy: 0.5}, 2.5)
	}
	before := state.emittedControls
	for i := 12; i < 24; i++ {
		state.addRawPoint(stereoPhosphorVertex{Position: stereoPhosphorPoint{X: float32(i), Y: 20}, Energy: 0.5}, 2.5)
	}
	if state.emittedControls <= before {
		t.Fatal("second batch did not continue the resampled curve")
	}
	state.resetCurve()
	if state.filteredCount != 0 || state.resampleValid || state.curveValid || state.emittedControls != 0 {
		t.Fatalf("curve reset retained geometry state: %+v", state)
	}
}

func TestStereoPhosphorFlushCompletesTailOnce(t *testing.T) {
	state := newStereoPhosphorTestState(128, 64)
	for i := 0; i < 28; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position:  stereoPhosphorPoint{X: 8 + float32(i)*3, Y: 32 + float32(math.Sin(float64(i)*0.25))*10},
			Energy:    0.5,
			Freshness: 0.4 + float32(i)/50,
		}, 2.5)
	}
	before := stereoPhosphorLayerSum(state.energyLayers[stereoPhosphorCurrentLayer])
	if !state.flushCurveTail(2.5) || !state.tailFlushed {
		t.Fatal("pending curve tail was not flushed")
	}
	after := stereoPhosphorLayerSum(state.energyLayers[stereoPhosphorCurrentLayer])
	if after <= before || state.resampleValid {
		t.Fatalf("tail did not drain: energy %.4f->%.4f resample=%v", before, after, state.resampleValid)
	}
	if state.flushCurveTail(2.5) {
		t.Fatal("tail flushed more than once")
	}
	second := stereoPhosphorLayerSum(state.energyLayers[stereoPhosphorCurrentLayer])
	if math.Abs(float64(second-after)) > 0.0001 {
		t.Fatalf("second flush changed energy: %.4f->%.4f", after, second)
	}
}

func TestStereoPhosphorNewInputStartsFreshChainAfterTailFlush(t *testing.T) {
	state := newStereoPhosphorTestState(96, 64)
	for i := 0; i < 20; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position:  stereoPhosphorPoint{X: 8 + float32(i)*3, Y: 30},
			Energy:    0.5,
			Freshness: 1,
		}, 2.5)
	}
	state.flushCurveTail(2.5)
	state.dc, state.bitmap = 1, 1
	state.lastSequence, state.sequenceValid = 100, true
	state.lastTick, state.tickValid = 2, true
	state.lastDisplayFrame, state.displayFrameValid = 0, true
	points := []StereoPoint{{X: 0.1, Y: 0.2}, {X: 0.15, Y: 0.25}}
	state.advance(points, 102, 3, 0, 96, 64)
	if state.tailFlushed || state.rawCount == 0 {
		t.Fatalf("resumed input did not start a fresh chain: flushed=%v raw=%d", state.tailFlushed, state.rawCount)
	}
}

func TestStereoPhosphorResamplingRetainsMergedExcitation(t *testing.T) {
	state := newStereoPhosphorTestState(128, 32)
	const inputEnergy = float32(0.01)
	const inputCount = 100
	for i := 0; i < inputCount; i++ {
		state.addRawPoint(stereoPhosphorVertex{
			Position: stereoPhosphorPoint{X: 4 + float32(i)*0.25, Y: 16},
			Energy:   inputEnergy,
		}, 2.5)
	}
	total := stereoPhosphorLayerSum(state.energyLayers[0])
	// A legacy sub-pixel path contributes roughly one unit here. The final
	// filter/resampler delay can retain a small suffix, but decimation must not
	// collapse the visible excitation with the control-point count.
	if total < 0.6 || total > 1.4 {
		t.Fatalf("resampled excitation = %.4f, want close to 1.0", total)
	}
}

func BenchmarkStereoPhosphorRender450(b *testing.B) {
	state := newStereoPhosphorTestState(450, 450)
	for i := 0; i < 360; i++ {
		angle := float64(i) * math.Pi / 180
		x := float32(225 + math.Cos(angle)*80)
		y := float32(225 + math.Sin(angle)*120)
		state.splat(x, y, 0.6)
	}
	colors := [4]uint32{rgb(38, 88, 105), rgb(60, 153, 181), rgb(116, 218, 238), rgb(210, 248, 255)}
	state.dc = 1 // Match the application's parallel DIB rendering path.
	state.renderPixels(colors[0], colors[1], colors[2], colors[3])
	b.Cleanup(func() {
		state.stopRenderPool()
		state.dc = 0
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.renderPixels(colors[0], colors[1], colors[2], colors[3])
	}
}

func BenchmarkStereoPhosphorCurves450(b *testing.B) {
	state := newStereoPhosphorTestState(450, 450)
	vertices := [4]stereoPhosphorVertex{
		{Position: stereoPhosphorPoint{X: 80, Y: 230}, Energy: 0.3, Freshness: 0.2},
		{Position: stereoPhosphorPoint{X: 210, Y: 80}, Energy: 0.4, Freshness: 0.4},
		{Position: stereoPhosphorPoint{X: 260, Y: 360}, Energy: 0.5, Freshness: 0.8},
		{Position: stereoPhosphorPoint{X: 390, Y: 190}, Energy: 0.5, Freshness: 1},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.rasterCatmullRom(vertices[0], vertices[1], vertices[2], vertices[3])
	}
}

func benchmarkStereoPhosphorStyleAdvance450(b *testing.B, style int) {
	state := newStereoPhosphorTestState(450, 450)
	state.dc, state.bitmap = 1, 1
	points := make([]StereoPoint, 480)
	for i := range points {
		angle := float64(i) * 2 * math.Pi / float64(len(points))
		radius := float32(0.15 + 0.55*float64((i%37)+1)/37)
		points[i] = StereoPoint{
			X: float32(math.Cos(angle)) * radius,
			Y: float32(math.Sin(angle)) * radius,
		}
	}
	var sequence, tick uint64
	state.advanceStyle(points, uint64(len(points)), 1, 0, style, 450, 450)
	state.advanceStyle(points, uint64(len(points))*2, 2, 0, style, 450, 450)
	sequence = uint64(len(points)) * 2
	tick = 2
	b.Cleanup(func() {
		state.stopRenderPool()
		state.dc, state.bitmap = 0, 0
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sequence += uint64(len(points))
		tick++
		state.advanceStyle(points, sequence, tick, 0, style, 450, 450)
	}
}

func BenchmarkStereoPhosphorDotAdvance450(b *testing.B) {
	benchmarkStereoPhosphorStyleAdvance450(b, 1)
}

func BenchmarkStereoPhosphorRayAdvance450(b *testing.B) {
	benchmarkStereoPhosphorStyleAdvance450(b, 2)
}

func TestStereoPhosphorSkipsComposeWhenSurfaceEmpty(t *testing.T) {
	for _, style := range []int{0, 1, 2} {
		state := newStereoPhosphorTestState(31, 31)
		state.dc, state.bitmap = 1, 1
		// Nothing was ever deposited: the first compose blanks the (already
		// zeroed) bitmap exactly once, every later compose tick must be skipped.
		blanked := 0
		for tick := uint64(1); tick <= 8; tick++ {
			_, composed := state.advanceStyle(nil, 0, tick, 0, style, 31, 31)
			if composed {
				blanked++
			}
		}
		if blanked != 1 {
			t.Fatalf("style %d: empty surface composed %d times, want exactly 1 blank clear", style, blanked)
		}
		// Depositing real energy must wake the composer again. Style 0's curve
		// filter buffers a few control points before emitting, so feed several
		// ticks and require at least one compose with visible pixels.
		var sequence uint64
		woke := false
		for tick := uint64(10); tick <= 16; tick++ {
			points := []StereoPoint{{X: 0.3, Y: 0.1}, {X: -0.1, Y: 0.4}, {X: 0.2, Y: -0.3}, {X: -0.35, Y: -0.15}}
			sequence += uint64(len(points))
			if _, composed := state.advanceStyle(points, sequence, tick, 0, style, 31, 31); composed {
				woke = true
			}
		}
		if !woke {
			t.Fatalf("style %d: deposits must resume composing", style)
		}
		visible := false
		for i := 3; i < len(state.pixels); i += 4 {
			if state.pixels[i] != 0 {
				visible = true
				break
			}
		}
		if !visible {
			t.Fatalf("style %d: composed frame has no visible pixels", style)
		}
		state.dc, state.bitmap = 0, 0
	}
}

func TestStereoPhosphorDecayEventuallySkipsCompose(t *testing.T) {
	state := newStereoPhosphorTestState(31, 31)
	state.dc, state.bitmap = 1, 1
	points := []StereoPoint{{X: 0.4, Y: 0.0}, {X: 0.0, Y: 0.4}}
	if _, composed := state.advanceStyle(points, 2, 2, 0, 0, 31, 31); !composed {
		t.Fatalf("initial deposit must compose")
	}
	// Feed silence; once the phosphor fully decays the composer must go quiet
	// and leave a fully transparent bitmap behind.
	lastComposedTick := uint64(2)
	for tick := uint64(3); tick <= 400; tick++ {
		_, composed := state.advanceStyle(nil, 2, tick, 0, 0, 31, 31)
		if composed {
			lastComposedTick = tick
		}
	}
	if lastComposedTick >= 399 {
		t.Fatalf("composer never went quiet, last compose at tick %d", lastComposedTick)
	}
	for i := 3; i < len(state.pixels); i += 4 {
		if state.pixels[i] != 0 {
			t.Fatalf("pixel %d alpha %d after full decay", i/4, state.pixels[i])
		}
	}
	state.dc, state.bitmap = 0, 0
}

func TestStereoPhosphorCornerStageSmoothsInteriorOnly(t *testing.T) {
	var stage stereoPhosphorCornerStage
	var out []stereoPhosphorVertex
	emit := func(v stereoPhosphorVertex) { out = append(out, v) }
	vertex := func(x, y float32) stereoPhosphorVertex {
		return stereoPhosphorVertex{Position: stereoPhosphorPoint{X: x, Y: y}}
	}

	// Straight line: interior points must stay exactly on the line.
	stage = stereoPhosphorCornerStage{}
	out = nil
	for i := 0; i < 5; i++ {
		stage.emit(vertex(float32(i*10), 0), emit)
	}
	stage.flush(emit)
	if len(out) != 5 {
		t.Fatalf("straight chain emitted %d points, want 5", len(out))
	}
	for i, v := range out {
		if v.Position.Y != 0 || math.Abs(float64(v.Position.X-float32(i*10))) > 0.0001 {
			t.Fatalf("straight chain point %d = %+v", i, v.Position)
		}
	}

	// 90-degree corner: the interior vertex is pulled toward the neighbors'
	// midpoint by the smooth factor; chain head and tail pass through raw.
	stage = stereoPhosphorCornerStage{}
	out = nil
	stage.emit(vertex(0, 0), emit)
	stage.emit(vertex(10, 0), emit)
	stage.emit(vertex(10, 10), emit)
	stage.flush(emit)
	if len(out) != 3 {
		t.Fatalf("corner chain emitted %d points, want 3", len(out))
	}
	if out[0].Position != (stereoPhosphorPoint{X: 0, Y: 0}) {
		t.Fatalf("chain head modified: %+v", out[0].Position)
	}
	if out[2].Position != (stereoPhosphorPoint{X: 10, Y: 10}) {
		t.Fatalf("chain tail modified: %+v", out[2].Position)
	}
	want := float32(10) - 5*stereoPhosphorCornerSmooth
	if math.Abs(float64(out[1].Position.X-want)) > 0.0001 ||
		math.Abs(float64(out[1].Position.Y-5*stereoPhosphorCornerSmooth)) > 0.0001 {
		t.Fatalf("corner vertex = %+v, want pull toward (5,5)", out[1].Position)
	}
	// Energy and freshness travel with the unsmoothed center vertex.
	corner := stereoPhosphorVertex{
		Position:  stereoPhosphorPoint{X: 10, Y: 0},
		Energy:    0.77,
		Freshness: 0.33,
	}
	stage = stereoPhosphorCornerStage{}
	out = nil
	stage.emit(vertex(0, 0), emit)
	stage.emit(corner, emit)
	stage.emit(vertex(10, 10), emit)
	stage.flush(emit)
	if out[1].Energy != 0.77 || out[1].Freshness != 0.33 {
		t.Fatalf("corner energy/freshness = %v/%v", out[1].Energy, out[1].Freshness)
	}
}

func TestStereoPhosphorCornerSmoothingDefaults(t *testing.T) {
	if stereoPhosphorCornerSmooth != 0.65 {
		t.Fatalf("corner smooth default = %v, want 0.65", stereoPhosphorCornerSmooth)
	}
	if stereoPhosphorCornerPasses != 2 {
		t.Fatalf("corner smoothing passes default = %d, want 2", stereoPhosphorCornerPasses)
	}
}

func TestStereoLineLowpassPassthroughWithoutRate(t *testing.T) {
	state := &stereoPhosphorState{}
	point := stereoPhosphorPoint{X: 12.5, Y: -7.25}
	if got := state.lowpassLinePoint(point); got != point {
		t.Fatalf("rate 0 passthrough = %+v, want %+v", got, point)
	}
	state.setLinePointRate(22050)
	restore := stereoLineLowpassHz
	stereoLineLowpassHz = 0
	defer func() { stereoLineLowpassHz = restore }()
	if got := state.lowpassLinePoint(point); got != point {
		t.Fatalf("cutoff 0 passthrough = %+v, want %+v", got, point)
	}
}

func TestStereoLineLowpassInitializesOnFirstPoint(t *testing.T) {
	state := &stereoPhosphorState{}
	state.setLinePointRate(22050)
	point := stereoPhosphorPoint{X: 30, Y: 40}
	if got := state.lowpassLinePoint(point); got != point {
		t.Fatalf("first point = %+v, want exact chain head %+v", got, point)
	}
}

func TestStereoLineLowpassAttenuatesAudioRateJitter(t *testing.T) {
	restore := stereoLineLowpassHz
	stereoLineLowpassHz = 16
	defer func() { stereoLineLowpassHz = restore }()
	state := &stereoPhosphorState{}
	state.setLinePointRate(22050)
	// A 220 Hz full-scale alternation stands in for audio-frequency fuzz. After
	// the cascade settles, the filtered excursion must be a small fraction of
	// the raw one while hovering at the signal mean.
	var maxExcursion float32
	for i := 0; i < 22050; i++ {
		x := float32(50)
		if i%2 == 1 {
			x = -50
		}
		got := state.lowpassLinePoint(stereoPhosphorPoint{X: x, Y: 0})
		if i > 11000 {
			if got.X < 0 {
				got.X = -got.X
			}
			if got.X > maxExcursion {
				maxExcursion = got.X
			}
		}
	}
	if maxExcursion > 5 {
		t.Fatalf("settled jitter excursion = %v, want < 5 (10%% of raw 50)", maxExcursion)
	}
}

func TestStereoLineLowpassTracksGestureAndResetBreaksChain(t *testing.T) {
	restore := stereoLineLowpassHz
	stereoLineLowpassHz = 16
	defer func() { stereoLineLowpassHz = restore }()
	state := &stereoPhosphorState{}
	state.setLinePointRate(22050)
	// A 2 Hz gesture sits far below the cutoff and must pass almost untouched
	// once the cascade settles.
	var last stereoPhosphorPoint
	for i := 0; i < 22050; i++ {
		angle := 2 * math.Pi * 2 * float64(i) / 22050
		last = state.lowpassLinePoint(stereoPhosphorPoint{
			X: float32(40 * math.Cos(angle)),
			Y: float32(40 * math.Sin(angle)),
		})
	}
	mag := float32(math.Sqrt(float64(last.X*last.X + last.Y*last.Y)))
	if mag < 38 || mag > 40.5 {
		t.Fatalf("gesture magnitude = %v, want ~40 (>= 95%% passthrough)", mag)
	}
	// A geometry discontinuity must restart the cascade exactly on the new
	// chain head instead of blending toward the old tail.
	state.resetCurve()
	head := stereoPhosphorPoint{X: -25, Y: -25}
	if got := state.lowpassLinePoint(head); got != head {
		t.Fatalf("post-reset head = %+v, want %+v", got, head)
	}
	if !state.lowpassValid {
		t.Fatal("low-pass chain should re-arm after the first post-reset point")
	}
}

func TestStereoLineLowpassDefaults(t *testing.T) {
	if stereoLineLowpassHz != 30 {
		t.Fatalf("low-pass cutoff default = %v, want 30", stereoLineLowpassHz)
	}
}

func TestStereoLineFreshnessIsTimeBasedAcrossBatches(t *testing.T) {
	restoreWindow := stereoLineFreshWindowSec
	defer func() { stereoLineFreshWindowSec = restoreWindow }()
	stereoLineFreshWindowSec = 0.08
	state := &stereoPhosphorState{}
	state.setLinePointRate(22050)
	// Two consecutive 120 Hz batches. With the legacy per-batch ramp the seam
	// between the newest point of batch one (1.0) and the oldest point of
	// batch two (~0.12) was a hard brightness step; time-based aging must keep
	// the seam nearly continuous.
	points := make([]StereoPoint, 184)
	for i := range points {
		points[i] = StereoPoint{X: float32(i) * 0.001, Y: 0.2}
	}
	state.advanceStyle(points, 184, 1, 0, 0, 96, 96)
	state.advanceStyle(points, 368, 2, 0, 0, 96, 96)
	// Walk the freshness assignment directly through the same code path: the
	// newest point of the first batch and the oldest point of the second batch
	// are one tick (184 points) apart in age, so their freshness must differ
	// by less than 0.2 instead of the legacy 0.88 jump.
	window := stereoLineFreshWindowSec
	rate := float32(22050)
	freshAt := func(agePoints uint64) float32 {
		age := float32(agePoints) / (rate * window)
		if age > 1 {
			age = 1
		}
		return 0.12 + 0.88*(1-age)*(1-age)
	}
	seam := freshAt(0) - freshAt(184)
	if seam > 0.2 {
		t.Fatalf("freshness seam across batches = %v, want < 0.2", seam)
	}
	// And points near the window edge must still ramp down to the floor.
	if freshAt(1600) >= 0.2 {
		t.Fatalf("aged freshness = %v, want near the 0.12 floor", freshAt(1600))
	}
}

func TestStereoLineFreshnessFallsBackWithoutRate(t *testing.T) {
	if stereoPhosphorAgeEnergy(0, 4) >= stereoPhosphorAgeEnergy(3, 4) {
		t.Fatal("legacy per-batch ramp must remain monotonic for the rate-less fallback")
	}
}

func TestStereoPhosphorCurveEnergyHasNoGapsOnStraightRuns(t *testing.T) {
	state := newStereoPhosphorTestState(72, 24)
	vertex := func(x float32) stereoPhosphorVertex {
		return stereoPhosphorVertex{
			Position:  stereoPhosphorPoint{X: x, Y: 10.35},
			Energy:    0.5,
			Freshness: 1,
		}
	}
	// A straight control run flattens to a single segment. Its energy must be
	// distributed along the whole stroke, not deposited only at the endpoint.
	state.rasterCatmullRom(vertex(3.2), vertex(20), vertex(40), vertex(68.4))
	for x := 21; x <= 39; x++ {
		if state.energyLayers[stereoPhosphorCurrentLayer][10*state.width+x] <= 0 {
			t.Fatalf("energy gap on straight run at x=%d", x)
		}
	}
}
