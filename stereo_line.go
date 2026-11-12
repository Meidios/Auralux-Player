//go:build windows

package main

import (
	"math"
	"unsafe"
)

const (
	stereoPhosphorLimit                = float32(1.50)
	stereoPhosphorInitialMax           = 256
	stereoPhosphorDotInitialMax        = 900
	stereoPhosphorDotMinCellSize       = 6
	stereoPhosphorDotMaxCellSize       = 10
	stereoPhosphorDotMinParticles      = 80
	stereoPhosphorDotMaxParticles      = 180
	stereoPhosphorDotDenseHits         = uint32(6)
	stereoPhosphorDotMinRadius         = float32(1.55)
	stereoPhosphorDotMaxRadius         = float32(2.20)
	stereoPhosphorDotFadeTicks         = uint64(60) // 500 ms at the 120 Hz analysis cadence
	stereoPhosphorDotHistoryDecay      = float32(0.86)
	stereoPhosphorDotHistoryInjection  = float32(0.34)
	stereoPhosphorDotCoverageDecay     = float32(0.84)
	stereoPhosphorDotCoverageInjection = float32(0.42)
	stereoPhosphorDotHighlightDecay    = float32(0.88)
	stereoPhosphorRayInitialMax        = 480
	stereoPhosphorRayBins              = 128
	stereoPhosphorRayHitScale          = float32(8.0)
	stereoPhosphorRayStart             = float32(0.04)
	stereoPhosphorRayMinStart          = float32(2.0)
	stereoPhosphorRayInnerFresh        = float32(0.12)
	// Keep the GONIO/LISSA LINE coordinate scale aligned with DOT and RAY.
	// The shared 0.82 layout margin and the soft radial knee below still keep
	// full-scale excursions inside the outer reference circle.
	stereoPhosphorArcBudget       = float32(12.0)
	stereoPhosphorBaseAlpha         = float32(0.08)
	stereoPhosphorMinSpacing        = float32(2.5)
	stereoPhosphorMaxSpacing        = float32(4.5)
	stereoPhosphorCurveFlatness     = float32(0.25)
	stereoPhosphorStrokeRadius      = float32(1.20)
	stereoPhosphorCurveMaxDepth     = 6
	stereoPhosphorResponseLUTSteps  = 4096
	stereoPhosphorRenderWorkers     = 4
	stereoPhosphorParallelPixels    = 65536
	stereoLineComposeDivisor        = uint64(2)
	stereoLineFadeTicks             = uint64(36) // 300 ms at the 120 Hz analysis cadence
	stereoLineTickEnergy            = float32(0.5)
	stereoPhosphorHistoryDecay      = float32(0.78)
	stereoPhosphorHistoryInjection  = float32(0.32)
	stereoPhosphorCoverageDecay     = float32(0.76)
	stereoPhosphorCoverageInjection = float32(0.38)
	stereoPhosphorHighlightDecay    = float32(0.82)
	stereoPhosphorHighlightAlpha    = float32(0.72)
	stereoPhosphorMinGlowRadius     = 8
	stereoPhosphorMaxGlowRadius     = 48
)

// Minimum control spacing around sharp turns. Pre-smoothing the densified
// turn controls used to be the only thing keeping corners readable; with the
// two-pass corner stage in place, a floor near the base spacing keeps turns
// rounder and deposits fewer redundant controls.
var stereoPhosphorCurveMinSpacing = float32(1.25)

// Display gain shared by every phosphor-backed stereo style (LINE / DOT / RAY
// all flow through stereoPhosphorMapPoint). 1.35 keeps ordinary programme
// material clear of the soft radial knee (0.90); only near-full-scale
// transients reach the limit. Variable so the render harness can A/B the
// magnification without editing constants.
var stereoLineDisplayGain = float32(1.35)

// stereoLineLowpassHz is the -3 dB point of the two-stage position low-pass
// applied to LINE-mode coordinates. Collected points arrive at half the audio
// sample rate (~22 kHz), so the raw path carries audio-frequency jitter that
// reads as a tangled ball of yarn once several hundred milliseconds of trail
// overlap. Cutting everything above the gesture band turns the trace into a
// smooth ribbon while beat-rate pans and width jumps still track. 30 Hz was
// picked from the off/8/16/30 Hz render A/B: it untangles the yarn while
// keeping a hint of oscilloscope texture. Only the position is filtered;
// energy and freshness stay snappy. 0 disables the filter. Variable so the
// render harness can A/B the cutoff.
var stereoLineLowpassHz = float32(30)

// stereoLineFreshWindowSec is the age window over which LINE freshness ramps
// from 1.0 down to the 0.12 floor, measured in seconds of stream time rather
// than batch position. The old per-batch ramp (0.12 -> 1.0 across each 120 Hz
// snapshot) was invisible while every tick traced a long zigzag; with the
// gesture low-pass each tick covers only a few pixels, so the ramp compressed
// into a sawtooth that read as a string of beads. Time-based aging keeps the
// seam between consecutive ticks continuous at any path speed. 80 ms keeps
// the per-tick seam step under 0.2 while still giving the head a mild recency
// emphasis; layer decay then handles the longer trail fade. 0 restores the
// legacy per-batch ramp (also the fallback when the point rate is unknown).
var stereoLineFreshWindowSec = float32(0.08)

func stereoLineFadeUntil(startTick uint64) uint64 {
	return startTick + stereoLineFadeTicks
}

func stereoStyleFadeUntil(startTick uint64, style int) uint64 {
	if style == 1 {
		return startTick + stereoPhosphorDotFadeTicks
	}
	return stereoLineFadeUntil(startTick)
}

func stereoLineFadeRunning(tick, until uint64) bool {
	return until > tick
}

const (
	stereoPhosphorCurrentLayer = 0
	stereoPhosphorHistoryLayer = 1
)

func stereoLineDisplayFrame(tick uint64) uint64 {
	if tick == 0 {
		return 0
	}
	return (tick - 1) / stereoLineComposeDivisor
}

func stereoLineCompletesFrame(tick uint64) bool {
	return tick > 0 && tick%stereoLineComposeDivisor == 0
}

func stereoLineShouldComposeFrame(tick, lastComposedTick uint64, composedValid bool) bool {
	if !composedValid {
		return stereoLineCompletesFrame(tick) || tick > 1
	}
	return stereoLineCompletesFrame(tick) || tick-lastComposedTick >= stereoLineComposeDivisor
}

type stereoPhosphorPoint struct {
	X float32
	Y float32
}

type stereoPhosphorVertex struct {
	Position  stereoPhosphorPoint
	Energy    float32
	Freshness float32
}

type stereoPhosphorCurveSample struct {
	Position stereoPhosphorPoint
	T        float32
}

type stereoPhosphorCurveSamples struct {
	values [(1 << stereoPhosphorCurveMaxDepth) + 1]stereoPhosphorCurveSample
	count  int
}

type stereoPhosphorRayBin struct {
	Endpoint  stereoPhosphorPoint
	Distance  float32
	Freshness float32
	Hits      uint16
}

type stereoPhosphorDotBin struct {
	SumX, SumY float32
	Freshness  float32
	Hits       uint32
	Seed       uint64
}

type stereoPhosphorDotCandidate struct {
	Index int
	Rank  uint64
}

type stereoPhosphorState struct {
	width, height     int
	energyLayers      [2][]float32
	coverageLayers    [2][]float32
	highlightEnergy   []float32
	compositeEnergy   []float32
	compositeCoverage []float32
	blurScratch       []float32
	glowEnergy        []float32
	pixels            []byte
	coreColorLUT      []uint32
	freshColorLUT     []uint32
	palette           [4]uint32
	paletteValid      bool
	renderPool        *stereoPhosphorRenderPool
	renderBlurRadius  int
	renderDiameter    int
	dc                uintptr
	bitmap            uintptr
	oldBitmap         uintptr

	lastSequence       uint64
	sequenceValid      bool
	lastTick           uint64
	tickValid          bool
	lastDisplayFrame   uint64
	displayFrameValid  bool
	lastComposedFrame  uint64
	lastComposedTick   uint64
	composedFrameValid bool
	// surfaceActive is set by every energy/coverage deposit and by ageHistory
	// when decayed history survives; composedBlank records that the bitmap is
	// already fully transparent. Together they let compose skip the whole
	// five-pass pixel pipeline when the surface holds no visible energy.
	surfaceActive bool
	composedBlank bool

	raw              [7]stereoPhosphorVertex
	rawCount         int
	filteredOlder    stereoPhosphorVertex
	filteredNewest   stereoPhosphorVertex
	filteredCount    int
	resampleFrom     stereoPhosphorVertex
	resampleValid    bool
	resampleDistance float32
	resampleEnergy   float32
	cornerA          stereoPhosphorCornerStage
	cornerB          stereoPhosphorCornerStage
	cornerC          stereoPhosphorCornerStage
	emittedControls  int
	smooth           [3]stereoPhosphorVertex
	smoothCount      int
	curveValid       bool
	tailFlushed      bool
	renderStyle      int

	dotCellSize     int
	dotColumns      int
	dotBins         []stereoPhosphorDotBin
	dotActive       []int
	dotCandidates   []stereoPhosphorDotCandidate
	dotDisplayFrame uint64
	dotFrameValid   bool

	// linePointRate is the collected LINE point rate in points per second
	// (half the audio sample rate). 0 disables the gesture low-pass, which
	// keeps sparse synthetic control streams in unit tests unfiltered.
	linePointRate float32
	lowpassValid  bool
	lowpassAX     float32
	lowpassAY     float32
	lowpassBX     float32
	lowpassBY     float32

	rayBins         [stereoPhosphorRayBins]stereoPhosphorRayBin
	rayDisplayFrame uint64
	rayFrameValid   bool
}

type stereoPhosphorResponseTables struct {
	core [stereoPhosphorResponseLUTSteps + 1]float32
	glow [stereoPhosphorResponseLUTSteps + 1]float32
}

var stereoPhosphorResponses = func() stereoPhosphorResponseTables {
	var tables stereoPhosphorResponseTables
	for i := 0; i <= stereoPhosphorResponseLUTSteps; i++ {
		value := float32(i) * stereoPhosphorLimit / stereoPhosphorResponseLUTSteps
		tables.core[i] = stereoPhosphorCoreAlpha(value)
		tables.glow[i] = stereoPhosphorGlowAlpha(value)
	}
	return tables
}()

type stereoPhosphorRenderPhase uint8

const (
	stereoPhosphorPhaseComposite stereoPhosphorRenderPhase = iota
	stereoPhosphorPhaseBlurHorizontal1
	stereoPhosphorPhaseBlurVertical1
	stereoPhosphorPhaseBlurHorizontal2
	stereoPhosphorPhaseFinalVertical
)

type stereoPhosphorRenderJob struct {
	phase      stereoPhosphorRenderPhase
	start, end int
}

type stereoPhosphorRenderPool struct {
	jobs   chan stereoPhosphorRenderJob
	done   chan struct{}
	exited chan struct{}
	count  int
}

func (s *stereoPhosphorState) ensureRenderPool() {
	if s.renderPool != nil {
		return
	}
	pool := &stereoPhosphorRenderPool{
		jobs:   make(chan stereoPhosphorRenderJob, stereoPhosphorRenderWorkers),
		done:   make(chan struct{}, stereoPhosphorRenderWorkers),
		exited: make(chan struct{}, stereoPhosphorRenderWorkers),
		count:  stereoPhosphorRenderWorkers,
	}
	s.renderPool = pool
	for i := 0; i < pool.count; i++ {
		go func() {
			for job := range pool.jobs {
				s.runRenderJob(job)
				pool.done <- struct{}{}
			}
			pool.exited <- struct{}{}
		}()
	}
}

func (s *stereoPhosphorState) stopRenderPool() {
	pool := s.renderPool
	if pool == nil {
		return
	}
	close(pool.jobs)
	for i := 0; i < pool.count; i++ {
		<-pool.exited
	}
	s.renderPool = nil
}

func (s *stereoPhosphorState) runRenderPhase(phase stereoPhosphorRenderPhase, total int) {
	pool := s.renderPool
	if pool == nil || total < 2 {
		s.runRenderJob(stereoPhosphorRenderJob{phase: phase, end: total})
		return
	}
	jobs := min(pool.count, total)
	for i := 0; i < jobs; i++ {
		start := total * i / jobs
		end := total * (i + 1) / jobs
		pool.jobs <- stereoPhosphorRenderJob{phase: phase, start: start, end: end}
	}
	for i := 0; i < jobs; i++ {
		<-pool.done
	}
}

func stereoPhosphorDotCellSize(width, height int) int {
	size := int(math.Round(float64(mini(width, height)) * 0.018))
	if size < stereoPhosphorDotMinCellSize {
		return stereoPhosphorDotMinCellSize
	}
	if size > stereoPhosphorDotMaxCellSize {
		return stereoPhosphorDotMaxCellSize
	}
	return size
}

func stereoPhosphorDotParticleLimit(width, height int) int {
	limit := int(math.Round(float64(width*height) / 1400.0))
	if limit < stereoPhosphorDotMinParticles {
		return stereoPhosphorDotMinParticles
	}
	if limit > stereoPhosphorDotMaxParticles {
		return stereoPhosphorDotMaxParticles
	}
	return limit
}

func (s *stereoPhosphorState) configureDotGrid() {
	cellSize := stereoPhosphorDotCellSize(s.width, s.height)
	columns := (s.width + cellSize - 1) / cellSize
	rows := (s.height + cellSize - 1) / cellSize
	binCount := columns * rows
	particleLimit := stereoPhosphorDotParticleLimit(s.width, s.height)
	if s.dotCellSize == cellSize && s.dotColumns == columns && len(s.dotBins) == binCount && cap(s.dotActive) >= binCount && cap(s.dotCandidates) >= particleLimit {
		s.resetDots()
		return
	}
	s.dotCellSize = cellSize
	s.dotColumns = columns
	s.dotBins = make([]stereoPhosphorDotBin, binCount)
	s.dotActive = make([]int, 0, binCount)
	s.dotCandidates = make([]stereoPhosphorDotCandidate, 0, particleLimit)
	s.dotDisplayFrame = 0
	s.dotFrameValid = false
}

func (s *stereoPhosphorState) resetDots() {
	for _, index := range s.dotActive {
		if index >= 0 && index < len(s.dotBins) {
			s.dotBins[index] = stereoPhosphorDotBin{}
		}
	}
	s.dotActive = s.dotActive[:0]
	s.dotCandidates = s.dotCandidates[:0]
	s.dotDisplayFrame = 0
	s.dotFrameValid = false
}

func (s *stereoPhosphorState) resetCurve() {
	s.rawCount = 0
	s.filteredCount = 0
	s.resampleValid = false
	s.resampleDistance = 0
	s.resampleEnergy = 0
	s.cornerA.count = 0
	s.cornerB.count = 0
	s.cornerC.count = 0
	s.emittedControls = 0
	s.smoothCount = 0
	s.curveValid = false
	s.tailFlushed = false
	// The low-pass chain belongs to the geometry stream: a discontinuity must
	// not pull the new segment toward the old one's tail. The rate estimate is
	// a stream property and survives.
	s.lowpassValid = false
}

func (s *stereoPhosphorState) resetRays() {
	clear(s.rayBins[:])
	s.rayDisplayFrame = 0
	s.rayFrameValid = false
}

// reset deliberately keeps the DIB allocation. Transport and mode changes are
// common UI operations; retaining the surface avoids a GDI allocation spike.
func (s *stereoPhosphorState) reset() {
	for i := range s.energyLayers {
		clear(s.energyLayers[i])
		clear(s.coverageLayers[i])
	}
	clear(s.compositeEnergy)
	clear(s.compositeCoverage)
	clear(s.highlightEnergy)
	clear(s.blurScratch)
	clear(s.glowEnergy)
	clear(s.pixels)
	s.sequenceValid = false
	s.tickValid = false
	s.displayFrameValid = false
	s.composedFrameValid = false
	s.surfaceActive = false
	s.composedBlank = false
	s.resetCurve()
	s.resetDots()
	s.resetRays()
}

func (s *stereoPhosphorState) destroy() {
	s.stopRenderPool()
	if s.dc != 0 {
		if s.bitmap != 0 {
			if s.oldBitmap != 0 {
				procSelectObject.Call(s.dc, s.oldBitmap)
			}
			procDeleteObject.Call(s.bitmap)
		}
		procDeleteDC.Call(s.dc)
	}
	s.dc, s.bitmap, s.oldBitmap = 0, 0, 0
	s.width, s.height = 0, 0
	for i := range s.energyLayers {
		s.energyLayers[i] = nil
		s.coverageLayers[i] = nil
	}
	s.highlightEnergy, s.compositeEnergy, s.compositeCoverage, s.blurScratch, s.glowEnergy, s.pixels = nil, nil, nil, nil, nil, nil
	s.resetDots()
	s.dotCellSize, s.dotColumns = 0, 0
	s.dotBins, s.dotActive, s.dotCandidates = nil, nil, nil
	s.sequenceValid = false
	s.tickValid = false
	s.displayFrameValid = false
	s.composedFrameValid = false
	s.surfaceActive = false
	s.composedBlank = false
	s.resetCurve()
	s.resetRays()
}

func (s *stereoPhosphorState) ensureSurface(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	if s.width == width && s.height == height && s.dc != 0 && s.bitmap != 0 {
		return true
	}
	s.destroy()
	bmi := BITMAPINFO{}
	bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(BITMAPINFOHEADER{}))
	bmi.BmiHeader.BiWidth = int32(width)
	bmi.BmiHeader.BiHeight = -int32(height)
	bmi.BmiHeader.BiPlanes = 1
	bmi.BmiHeader.BiBitCount = 32
	bmi.BmiHeader.BiCompression = BI_RGB
	var bits uintptr
	bitmap, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == 0 {
		return false
	}
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		procDeleteObject.Call(bitmap)
		return false
	}
	old, _, _ := procSelectObject.Call(dc, bitmap)
	if old == 0 {
		procDeleteDC.Call(dc)
		procDeleteObject.Call(bitmap)
		return false
	}
	s.width, s.height = width, height
	s.dc, s.bitmap, s.oldBitmap = dc, bitmap, old
	s.surfaceActive = false
	s.composedBlank = false
	for i := range s.energyLayers {
		s.energyLayers[i] = make([]float32, width*height)
		s.coverageLayers[i] = make([]float32, width*height)
	}
	s.highlightEnergy = make([]float32, width*height)
	s.compositeEnergy = make([]float32, width*height)
	s.compositeCoverage = make([]float32, width*height)
	s.blurScratch = make([]float32, width*height)
	s.glowEnergy = make([]float32, width*height)
	s.pixels = unsafe.Slice((*byte)(unsafe.Pointer(bits)), width*height*4)
	s.configureDotGrid()
	return true
}

func (s *stereoPhosphorState) newPointStart(pointCount int, sequence, _ uint64) (int, bool) {
	return s.newPointStartLimited(pointCount, sequence, stereoPhosphorInitialMax)
}

func (s *stereoPhosphorState) newPointStartLimited(pointCount int, sequence uint64, initialMax int) (int, bool) {
	if pointCount < 0 {
		pointCount = 0
	}
	if initialMax < 1 {
		initialMax = 1
	}
	if !s.sequenceValid {
		s.lastSequence = sequence
		s.sequenceValid = true
		start := pointCount - initialMax
		if start < 0 {
			start = 0
		}
		return start, true
	}
	if sequence < s.lastSequence {
		s.lastSequence = sequence
		return 0, true
	}
	delta := sequence - s.lastSequence
	s.lastSequence = sequence
	if delta == 0 {
		return pointCount, false
	}
	discontinuity := false
	if delta > uint64(pointCount) {
		delta = uint64(pointCount)
		discontinuity = true
	}
	// UI messages may coalesce while the analysis snapshot still contains every
	// new sample. Sequence continuity, rather than elapsed UI ticks, determines
	// whether the filter and curve state can be carried forward safely.
	return pointCount - int(delta), discontinuity
}

func (s *stereoPhosphorState) ageHistory(elapsed uint64) {
	if elapsed == 0 {
		return
	}
	historyDecay := stereoPhosphorHistoryDecay
	historyInjection := stereoPhosphorHistoryInjection
	coverageHistoryDecay := stereoPhosphorCoverageDecay
	coverageInjection := stereoPhosphorCoverageInjection
	highlightHistoryDecay := stereoPhosphorHighlightDecay
	if s.renderStyle == 1 {
		historyDecay = stereoPhosphorDotHistoryDecay
		historyInjection = stereoPhosphorDotHistoryInjection
		coverageHistoryDecay = stereoPhosphorDotCoverageDecay
		coverageInjection = stereoPhosphorDotCoverageInjection
		highlightHistoryDecay = stereoPhosphorDotHighlightDecay
	}
	energyDecay := float32(math.Pow(float64(historyDecay), float64(elapsed)))
	coverageDecay := float32(math.Pow(float64(coverageHistoryDecay), float64(elapsed)))
	currentEnergyDecay := float32(math.Pow(float64(historyDecay), float64(elapsed-1)))
	currentCoverageDecay := float32(math.Pow(float64(coverageHistoryDecay), float64(elapsed-1)))
	highlightDecay := float32(math.Pow(float64(highlightHistoryDecay), float64(elapsed)))
	currentHighlightDecay := float32(math.Pow(float64(highlightHistoryDecay), float64(elapsed-1)))
	currentEnergy := s.energyLayers[stereoPhosphorCurrentLayer]
	historyEnergy := s.energyLayers[stereoPhosphorHistoryLayer]
	currentCoverage := s.coverageLayers[stereoPhosphorCurrentLayer]
	historyCoverage := s.coverageLayers[stereoPhosphorHistoryLayer]
	alive := false
	for i := range currentEnergy {
		highlight := s.highlightEnergy[i] * highlightDecay
		injectedHighlight := currentCoverage[i] * currentHighlightDecay
		if injectedHighlight > highlight {
			highlight = injectedHighlight
		}
		if highlight < 0.002 {
			highlight = 0
		}
		s.highlightEnergy[i] = highlight
		energy := historyEnergy[i]*energyDecay + currentEnergy[i]*historyInjection*currentEnergyDecay
		if energy > stereoPhosphorLimit {
			energy = stereoPhosphorLimit
		} else if energy < 0.001 {
			energy = 0
		}
		historyEnergy[i] = energy
		coverage := historyCoverage[i] * coverageDecay
		injectedCoverage := currentCoverage[i] * coverageInjection * currentCoverageDecay
		if injectedCoverage > coverage {
			coverage = injectedCoverage
		}
		if coverage < 0.005 {
			coverage = 0
		}
		historyCoverage[i] = coverage
		if energy != 0 || coverage != 0 || highlight != 0 {
			alive = true
		}
	}
	if alive {
		s.surfaceActive = true
	}
	clear(currentEnergy)
	clear(currentCoverage)
}

func (s *stereoPhosphorState) moveToDisplayFrame(displayFrame uint64) (rewound bool) {
	if !s.displayFrameValid {
		s.lastDisplayFrame = displayFrame
		s.displayFrameValid = true
		return false
	}
	if displayFrame > s.lastDisplayFrame {
		s.surfaceActive = false
		s.ageHistory(displayFrame - s.lastDisplayFrame)
		s.lastDisplayFrame = displayFrame
		return false
	}
	if displayFrame < s.lastDisplayFrame {
		for i := range s.energyLayers {
			clear(s.energyLayers[i])
			clear(s.coverageLayers[i])
		}
		s.lastDisplayFrame = displayFrame
		s.composedFrameValid = false
		s.surfaceActive = false
		s.composedBlank = false
		s.resetCurve()
		return true
	}
	return false
}

func stereoPhosphorMapPoint(point StereoPoint, mode, width, height int) stereoPhosphorPoint {
	cx := float32(width-1) * 0.5
	cy := float32(height-1) * 0.5
	radius := float32(mini(width, height)-1) * 0.5
	if mode == 2 {
		l := (point.Y + point.X) * 0.70710678
		r := (point.Y - point.X) * 0.70710678
		den := float32(math.Abs(float64(l)) + math.Abs(float64(r)) + 0.000001)
		pan := (r - l) / den
		level := float32(math.Sqrt(float64((l*l+r*r)*0.5))) * stereoPolarDisplayGain
		angle := -math.Pi/2 + float64(pan)*(math.Pi/2)
		dx := float32(math.Cos(angle)) * level * radius * 0.92
		dy := float32(math.Sin(angle)) * level * radius * 0.92
		dx, dy = stereoPhosphorSoftLimit(dx, dy, radius)
		return stereoPhosphorPoint{X: cx + dx, Y: cy + dy}
	}
	x, y := point.X, point.Y
	if mode == 1 {
		l := (point.Y + point.X) * 0.70710678
		r := (point.Y - point.X) * 0.70710678
		x = (r - l) * 0.70710678
		y = (l + r) * 0.70710678
	} else {
		x = -point.X
	}
	scale := radius * 0.82 * stereoLineDisplayGain
	dx, dy := x*scale, y*scale
	dx, dy = stereoPhosphorSoftLimit(dx, dy, radius)
	return stereoPhosphorPoint{X: cx + dx, Y: cy - dy}
}

func stereoPhosphorSoftLimit(dx, dy, radius float32) (float32, float32) {
	knee := radius * 0.90
	limit := radius * 0.96
	// Sqrt of the squared magnitude is enough here: pixel-scale coordinates
	// never approach float32 overflow, and math.Hypot's scaling path is slow.
	mag := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if mag > knee && mag > 0 {
		rangeWidth := limit - knee
		compressed := knee + rangeWidth*float32(1-math.Exp(-float64((mag-knee)/rangeWidth)))
		factor := compressed / mag
		dx *= factor
		dy *= factor
	}
	return dx, dy
}

func stereoPhosphorDistance(a, b stereoPhosphorPoint) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}

func stereoPhosphorControlSpacing(width, height int) float32 {
	radius := float32(mini(width, height)-1) * 0.5
	spacing := radius * 0.016
	if spacing < stereoPhosphorMinSpacing {
		return stereoPhosphorMinSpacing
	}
	if spacing > stereoPhosphorMaxSpacing {
		return stereoPhosphorMaxSpacing
	}
	return spacing
}

func stereoPhosphorRasterSteps(from, to stereoPhosphorPoint) int {
	dx := math.Abs(float64(to.X - from.X))
	dy := math.Abs(float64(to.Y - from.Y))
	steps := int(math.Ceil(math.Max(dx, dy)))
	if steps < 1 {
		return 1
	}
	return steps
}

func stereoPhosphorAdaptiveSpacing(base float32, before, corner, after stereoPhosphorPoint) float32 {
	ax, ay := corner.X-before.X, corner.Y-before.Y
	bx, by := after.X-corner.X, after.Y-corner.Y
	aLength := float32(math.Sqrt(float64(ax*ax + ay*ay)))
	bLength := float32(math.Sqrt(float64(bx*bx + by*by)))
	if aLength < 0.001 || bLength < 0.001 {
		return base
	}
	cosine := (ax*bx + ay*by) / (aLength * bLength)
	if cosine < -1 {
		cosine = -1
	} else if cosine > 1 {
		cosine = 1
	}
	// Straight runs retain the normal control spacing. Tight turns receive up
	// to 55% denser controls so small loops remain curved instead of polygonal.
	turn := (1 - cosine) * 0.5
	spacing := base * (1 - 0.55*turn)
	if spacing < stereoPhosphorCurveMinSpacing {
		return stereoPhosphorCurveMinSpacing
	}
	return spacing
}

// stereoPhosphorArcStart limits visual complexity without simplifying the
// selected geometry. It keeps one contiguous suffix and always retains at
// least the newest segment, even when that segment alone exceeds the budget.
func stereoPhosphorArcStart(points []StereoPoint, start, mode, width, height int) (int, bool) {
	if start < 0 {
		start = 0
	}
	if start >= len(points)-1 {
		return start, false
	}
	radius := float32(mini(width, height)-1) * 0.5
	budget := radius * stereoPhosphorArcBudget
	newestSegmentStart := len(points) - 2
	previous := stereoPhosphorMapPoint(points[len(points)-1], mode, width, height)
	var length float32
	for i := len(points) - 2; i >= start; i-- {
		mapped := stereoPhosphorMapPoint(points[i], mode, width, height)
		length += stereoPhosphorDistance(mapped, previous)
		if length > budget {
			keptStart := i + 1
			if keptStart > newestSegmentStart {
				keptStart = newestSegmentStart
			}
			return keptStart, keptStart > start
		}
		previous = mapped
	}
	return start, false
}

func stereoPhosphorAgeEnergy(index, count int) float32 {
	if count <= 1 {
		return 1
	}
	t := float32(index) / float32(count-1)
	return 0.12 + 0.88*t*t
}

func (s *stereoPhosphorState) depositEnergy(x, y, amount float32) {
	s.surfaceActive = true
	ix, iy := int(math.Floor(float64(x))), int(math.Floor(float64(y)))
	fx, fy := x-float32(ix), y-float32(iy)
	weights := [4]struct {
		dx, dy int
		weight float32
	}{
		{0, 0, (1 - fx) * (1 - fy)},
		{1, 0, fx * (1 - fy)},
		{0, 1, (1 - fx) * fy},
		{1, 1, fx * fy},
	}
	for _, sample := range weights {
		px, py := ix+sample.dx, iy+sample.dy
		if px < 0 || py < 0 || px >= s.width || py >= s.height || sample.weight <= 0 {
			continue
		}
		index := py*s.width + px
		value := s.energyLayers[0][index] + amount*sample.weight
		if value > stereoPhosphorLimit {
			value = stereoPhosphorLimit
		}
		s.energyLayers[0][index] = value
	}
}

func (s *stereoPhosphorState) depositCoverage(x, y, amount float32) {
	s.surfaceActive = true
	ix, iy := int(math.Floor(float64(x))), int(math.Floor(float64(y)))
	fx, fy := x-float32(ix), y-float32(iy)
	weights := [4]struct {
		dx, dy int
		weight float32
	}{
		{0, 0, (1 - fx) * (1 - fy)},
		{1, 0, fx * (1 - fy)},
		{0, 1, (1 - fx) * fy},
		{1, 1, fx * fy},
	}
	for _, sample := range weights {
		px, py := ix+sample.dx, iy+sample.dy
		if px < 0 || py < 0 || px >= s.width || py >= s.height || sample.weight <= 0 {
			continue
		}
		// Square-root coverage keeps a one-pixel beam visually continuous at
		// fractional coordinates without turning it into a heavy two-pixel line.
		coverage := amount * float32(math.Sqrt(float64(sample.weight)))
		index := py*s.width + px
		if coverage > s.coverageLayers[stereoPhosphorCurrentLayer][index] {
			s.coverageLayers[stereoPhosphorCurrentLayer][index] = coverage
		}
	}
}

func (s *stereoPhosphorState) rasterPointCoverage(point stereoPhosphorPoint, radius, freshness float32) {
	freshness = stereoPhosphorClamp(freshness, 0, 1)
	if freshness > 0 {
		s.surfaceActive = true
	}
	extent := radius + 0.5
	minX := max(0, int(math.Floor(float64(point.X-extent))))
	maxX := min(s.width-1, int(math.Ceil(float64(point.X+extent))))
	minY := max(0, int(math.Floor(float64(point.Y-extent))))
	maxY := min(s.height-1, int(math.Ceil(float64(point.Y+extent))))
	for y := minY; y <= maxY; y++ {
		dy := float32(y) - point.Y
		for x := minX; x <= maxX; x++ {
			dx := float32(x) - point.X
			distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			coverage := stereoPhosphorClamp(radius+0.5-distance, 0, 1) * freshness
			if coverage <= 0 {
				continue
			}
			index := y*s.width + x
			if coverage > s.coverageLayers[stereoPhosphorCurrentLayer][index] {
				s.coverageLayers[stereoPhosphorCurrentLayer][index] = coverage
			}
		}
	}
}

func stereoPhosphorClamp(value, low, high float32) float32 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// rasterStrokeSegment writes analytic, anti-aliased coverage for a round
// capsule. Coverage is continuous along the segment, so the bright core does
// not reveal the individual curve samples as a dotted chain.
func (s *stereoPhosphorState) rasterStrokeSegment(from, to stereoPhosphorPoint, freshnessFrom, freshnessTo float32) {
	freshnessFrom = stereoPhosphorClamp(freshnessFrom, 0, 1)
	freshnessTo = stereoPhosphorClamp(freshnessTo, 0, 1)
	if freshnessFrom <= 0 && freshnessTo <= 0 {
		return
	}
	s.surfaceActive = true
	dx, dy := to.X-from.X, to.Y-from.Y
	lengthSquared := dx*dx + dy*dy
	extent := stereoPhosphorStrokeRadius + 0.5
	minX := int(math.Floor(float64(min(from.X, to.X) - extent)))
	maxX := int(math.Ceil(float64(max(from.X, to.X) + extent)))
	minY := int(math.Floor(float64(min(from.Y, to.Y) - extent)))
	maxY := int(math.Ceil(float64(max(from.Y, to.Y) + extent)))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= s.width {
		maxX = s.width - 1
	}
	if maxY >= s.height {
		maxY = s.height - 1
	}
	for y := minY; y <= maxY; y++ {
		py := float32(y)
		dy0 := py - from.Y
		for x := minX; x <= maxX; x++ {
			px := float32(x)
			t := float32(0)
			if lengthSquared > 0.000001 {
				t = stereoPhosphorClamp(((px-from.X)*dx+(dy0)*dy)/lengthSquared, 0, 1)
			}
			closestX, closestY := from.X+dx*t, from.Y+dy*t
			cdx, cdy := px-closestX, py-closestY
			distance := float32(math.Sqrt(float64(cdx*cdx + cdy*cdy)))
			freshness := freshnessFrom + (freshnessTo-freshnessFrom)*t
			coverage := stereoPhosphorClamp(stereoPhosphorStrokeRadius+0.5-distance, 0, 1) * freshness
			if coverage <= 0 {
				continue
			}
			index := y*s.width + x
			if coverage > s.coverageLayers[stereoPhosphorCurrentLayer][index] {
				s.coverageLayers[stereoPhosphorCurrentLayer][index] = coverage
			}
		}
	}
}

func (s *stereoPhosphorState) splat(x, y, amount float32) {
	point := stereoPhosphorPoint{X: x, Y: y}
	s.rasterStrokeSegment(point, point, 1, 1)
	s.depositEnergy(x, y, amount)
}

func (s *stereoPhosphorState) splatFresh(point stereoPhosphorPoint, energy, freshness float32) {
	s.splatDotParticle(point, stereoPhosphorDotMinRadius, energy, freshness)
}

func (s *stereoPhosphorState) splatDotParticle(point stereoPhosphorPoint, radius, energy, freshness float32) {
	s.rasterPointCoverage(point, radius, freshness)
	s.depositEnergy(point.X, point.Y, energy)
}

func stereoPhosphorMixSequence(value uint64) uint64 {
	// SplitMix64 gives stable, well-distributed particle choices without a
	// mutable RNG. The same audio sample therefore never flickers between
	// positions when UI messages are coalesced or repainted.
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func stereoPhosphorPointSequence(sequence uint64, pointCount, index int) uint64 {
	if pointCount <= 0 || index < 0 || index >= pointCount {
		return sequence
	}
	distanceFromNewest := uint64(pointCount - 1 - index)
	if distanceFromNewest > sequence {
		return uint64(index + 1)
	}
	return sequence - distanceFromNewest
}

func stereoPhosphorHashUnit(hash uint64, shift uint) float32 {
	return float32((hash>>shift)&0xffff) / 65535
}

func stereoPhosphorDotScatterScale(width, height int) float32 {
	scale := float32(mini(width, height)) / 450
	return stereoPhosphorClamp(scale, 0.75, 1.35)
}

func stereoPhosphorDotParticle(seed uint64, width, height int, hits uint32, halo bool) (offset stereoPhosphorPoint, radius, energy float32) {
	hash := stereoPhosphorMixSequence(seed)
	scale := stereoPhosphorDotScatterScale(width, height)
	uDistance := stereoPhosphorHashUnit(hash, 16)
	uAngle := stereoPhosphorHashUnit(hash, 32)
	uRadius := stereoPhosphorHashUnit(hash, 48)
	uEnergy := stereoPhosphorHashUnit(stereoPhosphorMixSequence(hash), 16)
	minDistance, maxDistance := float32(2), float32(5)
	if halo {
		minDistance, maxDistance = 10, 22
	} else {
		tier := hash & 255
		if tier >= 51 && tier < 204 {
			minDistance, maxDistance = 5, 12
		} else if tier >= 204 {
			minDistance, maxDistance = 12, 18
		}
	}
	minDistance *= scale
	maxDistance *= scale
	distance := float32(math.Sqrt(float64(minDistance*minDistance + uDistance*(maxDistance*maxDistance-minDistance*minDistance))))
	angle := float64(uAngle) * 2 * math.Pi
	offset = stereoPhosphorPoint{X: float32(math.Cos(angle)) * distance, Y: float32(math.Sin(angle)) * distance}
	densityBoost := float32(math.Log2(float64(hits)+1)) * 0.04
	if halo {
		radius = stereoPhosphorDotMinRadius + 0.40*uRadius
		energy = 0.35 + 0.25*uEnergy + stereoPhosphorMin(densityBoost*0.30, 0.05)
		return offset, radius, stereoPhosphorMin(energy, 0.65)
	}
	radius = stereoPhosphorDotMinRadius + (stereoPhosphorDotMaxRadius-stereoPhosphorDotMinRadius)*uRadius
	energy = 0.90 + 0.20*uEnergy + stereoPhosphorMin(densityBoost, 0.15)
	return offset, radius, stereoPhosphorMin(energy, 1.25)
}

func (s *stereoPhosphorState) addDotPoint(point stereoPhosphorPoint, freshness float32, sequence, displayFrame uint64) {
	if len(s.dotBins) == 0 || s.dotColumns == 0 || s.dotCellSize == 0 {
		s.configureDotGrid()
	}
	if len(s.dotBins) == 0 || point.X < 0 || point.Y < 0 || point.X >= float32(s.width) || point.Y >= float32(s.height) {
		return
	}
	if !s.dotFrameValid {
		s.dotDisplayFrame = displayFrame
		s.dotFrameValid = true
	}
	column := int(point.X) / s.dotCellSize
	row := int(point.Y) / s.dotCellSize
	index := row*s.dotColumns + column
	if index < 0 || index >= len(s.dotBins) {
		return
	}
	bin := &s.dotBins[index]
	if bin.Hits == 0 {
		s.dotActive = append(s.dotActive, index)
	}
	if bin.Hits < ^uint32(0) {
		bin.Hits++
	}
	bin.SumX += point.X
	bin.SumY += point.Y
	if freshness > bin.Freshness {
		bin.Freshness = freshness
	}
	bin.Seed ^= stereoPhosphorMixSequence(sequence)
}

func stereoPhosphorDotCandidateWorse(a, b stereoPhosphorDotCandidate) bool {
	if a.Rank != b.Rank {
		return a.Rank > b.Rank
	}
	return a.Index > b.Index
}

func (s *stereoPhosphorState) addDotCandidate(candidate stereoPhosphorDotCandidate, limit int) {
	if limit <= 0 {
		return
	}
	if len(s.dotCandidates) < limit {
		s.dotCandidates = append(s.dotCandidates, candidate)
		child := len(s.dotCandidates) - 1
		for child > 0 {
			parent := (child - 1) / 2
			if !stereoPhosphorDotCandidateWorse(s.dotCandidates[child], s.dotCandidates[parent]) {
				break
			}
			s.dotCandidates[child], s.dotCandidates[parent] = s.dotCandidates[parent], s.dotCandidates[child]
			child = parent
		}
		return
	}
	if !stereoPhosphorDotCandidateWorse(s.dotCandidates[0], candidate) {
		return
	}
	s.dotCandidates[0] = candidate
	for parent := 0; ; {
		left := parent*2 + 1
		if left >= len(s.dotCandidates) {
			break
		}
		worse := left
		right := left + 1
		if right < len(s.dotCandidates) && stereoPhosphorDotCandidateWorse(s.dotCandidates[right], s.dotCandidates[left]) {
			worse = right
		}
		if !stereoPhosphorDotCandidateWorse(s.dotCandidates[worse], s.dotCandidates[parent]) {
			break
		}
		s.dotCandidates[parent], s.dotCandidates[worse] = s.dotCandidates[worse], s.dotCandidates[parent]
		parent = worse
	}
}

func (s *stereoPhosphorState) flushDotBins() int {
	if len(s.dotActive) == 0 {
		s.dotFrameValid = false
		return 0
	}
	particleLimit := stereoPhosphorDotParticleLimit(s.width, s.height)
	primaryLimit := max(1, particleLimit*3/4)
	s.dotCandidates = s.dotCandidates[:0]
	for _, index := range s.dotActive {
		bin := s.dotBins[index]
		weight := uint64(1 + int(math.Sqrt(float64(bin.Hits)))*4)
		seed := stereoPhosphorMixSequence(bin.Seed ^ stereoPhosphorMixSequence(s.dotDisplayFrame+uint64(index)+1))
		s.addDotCandidate(stereoPhosphorDotCandidate{Index: index, Rank: seed / weight}, primaryLimit)
	}
	for i := 1; i < len(s.dotCandidates); i++ {
		candidate := s.dotCandidates[i]
		j := i - 1
		for j >= 0 && stereoPhosphorDotCandidateWorse(s.dotCandidates[j], candidate) {
			s.dotCandidates[j+1] = s.dotCandidates[j]
			j--
		}
		s.dotCandidates[j+1] = candidate
	}
	emitted := 0
	for _, candidate := range s.dotCandidates {
		bin := s.dotBins[candidate.Index]
		if bin.Hits == 0 {
			continue
		}
		center := stereoPhosphorPoint{X: bin.SumX / float32(bin.Hits), Y: bin.SumY / float32(bin.Hits)}
		seed := bin.Seed ^ stereoPhosphorMixSequence(s.dotDisplayFrame+uint64(candidate.Index)+1)
		offset, radius, energy := stereoPhosphorDotParticle(seed, s.width, s.height, bin.Hits, false)
		freshness := stereoPhosphorClamp(bin.Freshness, 0, 1)
		s.splatDotParticle(
			stereoPhosphorPoint{X: center.X + offset.X, Y: center.Y + offset.Y},
			radius,
			energy*freshness,
			freshness*float32(math.Sqrt(float64(energy/1.25))),
		)
		emitted++
	}
	for _, candidate := range s.dotCandidates {
		if emitted >= particleLimit {
			break
		}
		bin := s.dotBins[candidate.Index]
		if bin.Hits < stereoPhosphorDotDenseHits {
			continue
		}
		center := stereoPhosphorPoint{X: bin.SumX / float32(bin.Hits), Y: bin.SumY / float32(bin.Hits)}
		seed := bin.Seed ^ stereoPhosphorMixSequence(s.dotDisplayFrame+uint64(candidate.Index)+0x9e3779b9)
		offset, radius, energy := stereoPhosphorDotParticle(seed, s.width, s.height, bin.Hits, true)
		freshness := stereoPhosphorClamp(bin.Freshness*0.78, 0, 1)
		s.splatDotParticle(
			stereoPhosphorPoint{X: center.X + offset.X, Y: center.Y + offset.Y},
			radius,
			energy*freshness,
			freshness*float32(math.Sqrt(float64(energy/0.65))),
		)
		emitted++
	}
	for _, index := range s.dotActive {
		s.dotBins[index] = stereoPhosphorDotBin{}
	}
	s.dotActive = s.dotActive[:0]
	s.dotCandidates = s.dotCandidates[:0]
	s.dotFrameValid = false
	return emitted
}

func (s *stereoPhosphorState) rasterSegment(from, to stereoPhosphorPoint, energy float32) {
	dx, dy := to.X-from.X, to.Y-from.Y
	length := stereoPhosphorDistance(from, to)
	steps := int(math.Ceil(float64(length / stereoPhosphorCurveFlatness)))
	if steps < 1 {
		steps = 1
	}
	amount := energy * float32(math.Sqrt(float64(max(length, 1)))) / float32(steps)
	previous := from
	for i := 1; i <= steps; i++ {
		t := float32(i) / float32(steps)
		point := stereoPhosphorPoint{X: from.X + dx*t, Y: from.Y + dy*t}
		s.rasterStrokeSegment(previous, point, 1, 1)
		s.depositEnergy(point.X, point.Y, amount)
		previous = point
	}
}

func (s *stereoPhosphorState) rasterTaperedSegment(from, to stereoPhosphorPoint, energy, freshnessFrom, freshnessTo float32) {
	dx, dy := to.X-from.X, to.Y-from.Y
	length := stereoPhosphorDistance(from, to)
	steps := stereoPhosphorRasterSteps(from, to)
	amount := energy * float32(math.Sqrt(float64(max(length, 1)))) / float32(steps)
	// Step once per dominant-axis pixel. Direct bilinear coverage is both
	// continuous and much cheaper than testing every pixel in the large
	// axis-aligned bounding box of a diagonal ray.
	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		point := stereoPhosphorPoint{X: from.X + dx*t, Y: from.Y + dy*t}
		freshness := freshnessFrom + (freshnessTo-freshnessFrom)*t
		s.depositCoverage(point.X, point.Y, freshness)
		// Keep the origin restrained while allowing the endpoint to carry the
		// newest directional energy. This prevents dense RAY material from
		// collapsing into a white center knot.
		s.depositEnergy(point.X, point.Y, amount*(0.25+0.75*t))
	}
}

func stereoPhosphorCenter(width, height int) stereoPhosphorPoint {
	return stereoPhosphorPoint{X: float32(width-1) * 0.5, Y: float32(height-1) * 0.5}
}

func (s *stereoPhosphorState) addRayPoint(point, center stereoPhosphorPoint, freshness float32) {
	dx, dy := point.X-center.X, point.Y-center.Y
	distance := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if distance < 0.5 {
		return
	}
	angle := math.Atan2(float64(dy), float64(dx))
	if angle < 0 {
		angle += 2 * math.Pi
	}
	index := int(angle / (2 * math.Pi) * stereoPhosphorRayBins)
	if index >= stereoPhosphorRayBins {
		index = stereoPhosphorRayBins - 1
	}
	bin := &s.rayBins[index]
	if bin.Hits < ^uint16(0) {
		bin.Hits++
	}
	if distance > bin.Distance {
		bin.Endpoint = point
		bin.Distance = distance
	}
	if freshness > bin.Freshness {
		bin.Freshness = freshness
	}
}

func (s *stereoPhosphorState) flushRayBins(center stereoPhosphorPoint) bool {
	drew := false
	for i := range s.rayBins {
		bin := &s.rayBins[i]
		if bin.Hits == 0 || bin.Distance < 0.5 {
			continue
		}
		startDistance := max(stereoPhosphorRayMinStart, bin.Distance*stereoPhosphorRayStart)
		if startDistance >= bin.Distance {
			startDistance = bin.Distance * 0.5
		}
		factor := startDistance / bin.Distance
		start := stereoPhosphorPoint{
			X: center.X + (bin.Endpoint.X-center.X)*factor,
			Y: center.Y + (bin.Endpoint.Y-center.Y)*factor,
		}
		hitResponse := float32(1 - math.Exp(-float64(float32(bin.Hits)/stereoPhosphorRayHitScale)))
		freshness := stereoPhosphorClamp(bin.Freshness, 0, 1)
		s.rasterTaperedSegment(
			start,
			bin.Endpoint,
			stereoLineTickEnergy*hitResponse,
			freshness*stereoPhosphorRayInnerFresh,
			freshness,
		)
		drew = true
	}
	clear(s.rayBins[:])
	return drew
}

func stereoPhosphorLimitedTangent(tangent stereoPhosphorPoint, segmentLength float32) stereoPhosphorPoint {
	length := float32(math.Sqrt(float64(tangent.X*tangent.X + tangent.Y*tangent.Y)))
	limit := segmentLength * 1.5
	if length <= limit || length < 0.0001 {
		return tangent
	}
	scale := limit / length
	return stereoPhosphorPoint{X: tangent.X * scale, Y: tangent.Y * scale}
}

func stereoPhosphorSubtract(a, b stereoPhosphorPoint) stereoPhosphorPoint {
	return stereoPhosphorPoint{X: a.X - b.X, Y: a.Y - b.Y}
}

func stereoPhosphorScale(point stereoPhosphorPoint, scale float32) stereoPhosphorPoint {
	return stereoPhosphorPoint{X: point.X * scale, Y: point.Y * scale}
}

func stereoPhosphorAdd(a, b stereoPhosphorPoint) stereoPhosphorPoint {
	return stereoPhosphorPoint{X: a.X + b.X, Y: a.Y + b.Y}
}

func stereoPhosphorCentripetalStep(a, b stereoPhosphorPoint) float32 {
	// Catmull-Rom alpha=0.5: parameter increments are the square root of
	// screen-space chord length. Keep duplicate endpoint guards finite.
	step := float32(math.Sqrt(float64(stereoPhosphorDistance(a, b))))
	if step < 0.001 {
		return 0.001
	}
	return step
}

func stereoPhosphorCentripetalTangents(p0, p1, p2, p3 stereoPhosphorPoint) (stereoPhosphorPoint, stereoPhosphorPoint) {
	// Extend duplicated end points instead of allowing a near-zero parameter
	// interval to create an unstable first or last tangent.
	if stereoPhosphorDistance(p0, p1) < 0.001 {
		p0 = stereoPhosphorAdd(p1, stereoPhosphorSubtract(p1, p2))
	}
	if stereoPhosphorDistance(p2, p3) < 0.001 {
		p3 = stereoPhosphorAdd(p2, stereoPhosphorSubtract(p2, p1))
	}
	dt10 := stereoPhosphorCentripetalStep(p0, p1)
	dt21 := stereoPhosphorCentripetalStep(p1, p2)
	dt32 := stereoPhosphorCentripetalStep(p2, p3)
	dt20 := dt10 + dt21
	dt31 := dt21 + dt32

	m1 := stereoPhosphorAdd(
		stereoPhosphorSubtract(
			stereoPhosphorScale(stereoPhosphorSubtract(p1, p0), 1/dt10),
			stereoPhosphorScale(stereoPhosphorSubtract(p2, p0), 1/dt20),
		),
		stereoPhosphorScale(stereoPhosphorSubtract(p2, p1), 1/dt21),
	)
	m2 := stereoPhosphorAdd(
		stereoPhosphorSubtract(
			stereoPhosphorScale(stereoPhosphorSubtract(p2, p1), 1/dt21),
			stereoPhosphorScale(stereoPhosphorSubtract(p3, p1), 1/dt31),
		),
		stereoPhosphorScale(stereoPhosphorSubtract(p3, p2), 1/dt32),
	)
	return stereoPhosphorScale(m1, dt21), stereoPhosphorScale(m2, dt21)
}

func stereoPhosphorMidpoint(a, b stereoPhosphorPoint) stereoPhosphorPoint {
	return stereoPhosphorPoint{X: (a.X + b.X) * 0.5, Y: (a.Y + b.Y) * 0.5}
}

func stereoPhosphorPointLineDistance(point, from, to stereoPhosphorPoint) float32 {
	dx, dy := to.X-from.X, to.Y-from.Y
	lengthSquared := dx*dx + dy*dy
	if lengthSquared <= 0.000001 {
		return stereoPhosphorDistance(point, from)
	}
	t := stereoPhosphorClamp(((point.X-from.X)*dx+(point.Y-from.Y)*dy)/lengthSquared, 0, 1)
	projection := stereoPhosphorPoint{X: from.X + dx*t, Y: from.Y + dy*t}
	return stereoPhosphorDistance(point, projection)
}

func stereoPhosphorFlattenCubic(samples *stereoPhosphorCurveSamples, p0, c1, c2, p3 stereoPhosphorPoint, t0, t1 float32, depth int) {
	flatness := max(
		stereoPhosphorPointLineDistance(c1, p0, p3),
		stereoPhosphorPointLineDistance(c2, p0, p3),
	)
	if flatness <= stereoPhosphorCurveFlatness || depth >= stereoPhosphorCurveMaxDepth {
		if samples.count < len(samples.values) {
			samples.values[samples.count] = stereoPhosphorCurveSample{Position: p3, T: t1}
			samples.count++
		}
		return
	}
	p01 := stereoPhosphorMidpoint(p0, c1)
	p12 := stereoPhosphorMidpoint(c1, c2)
	p23 := stereoPhosphorMidpoint(c2, p3)
	p012 := stereoPhosphorMidpoint(p01, p12)
	p123 := stereoPhosphorMidpoint(p12, p23)
	midpoint := stereoPhosphorMidpoint(p012, p123)
	tMid := (t0 + t1) * 0.5
	stereoPhosphorFlattenCubic(samples, p0, p01, p012, midpoint, t0, tMid, depth+1)
	stereoPhosphorFlattenCubic(samples, midpoint, p123, p23, p3, tMid, t1, depth+1)
}

func (s *stereoPhosphorState) rasterCatmullRom(p0, p1, p2, p3 stereoPhosphorVertex) {
	segmentLength := stereoPhosphorDistance(p1.Position, p2.Position)
	m1, m2 := stereoPhosphorCentripetalTangents(p0.Position, p1.Position, p2.Position, p3.Position)
	m1 = stereoPhosphorLimitedTangent(m1, segmentLength)
	m2 = stereoPhosphorLimitedTangent(m2, segmentLength)
	c1 := stereoPhosphorPoint{X: p1.Position.X + m1.X/3, Y: p1.Position.Y + m1.Y/3}
	c2 := stereoPhosphorPoint{X: p2.Position.X - m2.X/3, Y: p2.Position.Y - m2.Y/3}
	samples := stereoPhosphorCurveSamples{}
	samples.values[0] = stereoPhosphorCurveSample{Position: p1.Position}
	samples.count = 1
	stereoPhosphorFlattenCubic(&samples, p1.Position, c1, c2, p2.Position, 0, 1, 0)
	curveLength := float32(0)
	for i := 1; i < samples.count; i++ {
		curveLength += stereoPhosphorDistance(samples.values[i-1].Position, samples.values[i].Position)
	}
	segmentCount := max(samples.count-1, 1)
	amountScale := float32(math.Sqrt(float64(max(curveLength, 1)))) / float32(segmentCount)
	for i := 1; i < samples.count; i++ {
		previous := samples.values[i-1].Position
		point := samples.values[i].Position
		previousT := samples.values[i-1].T
		t := samples.values[i].T
		energy := p1.Energy + (p2.Energy-p1.Energy)*t
		freshnessFrom := p1.Freshness + (p2.Freshness-p1.Freshness)*previousT
		freshnessTo := p1.Freshness + (p2.Freshness-p1.Freshness)*t
		s.rasterStrokeSegment(previous, point, freshnessFrom, freshnessTo)
		// Deposit the segment's energy along the stroke instead of only at the
		// flattened sample point. Straight runs flatten to a single segment, so
		// endpoint-only deposits read as discrete beads once the gesture
		// low-pass removes the curvature that used to subdivide them.
		steps := stereoPhosphorRasterSteps(previous, point)
		per := energy * amountScale / float32(steps)
		for k := 1; k <= steps; k++ {
			u := float32(k) / float32(steps)
			s.depositEnergy(previous.X+(point.X-previous.X)*u, previous.Y+(point.Y-previous.Y)*u, per)
		}
	}
}

// stereoPhosphorCornerSmooth blends each interior resampled control point
// toward the midpoint of its neighbors, cutting polygon corners before the
// Catmull-Rom stage. A 0.65 blend rounds sustained body curves enough to cut
// visible stair steps while retaining transient excursions and overall shape.
// Stroke count and energy are untouched; only control geometry is smoothed.
// Two chained passes target noise whose wiggle wavelength spans several
// control spacings without the shape softening seen at three passes.
var (
	stereoPhosphorCornerSmooth = float32(0.65)
	stereoPhosphorCornerPasses = 2
)

// stereoPhosphorCornerStage is one [1,2,1] smoothing pass over the resampled
// control stream with a one-point look-ahead. Chain heads and tails pass
// through unsmoothed so the curve starts and ends exactly on the signal.
type stereoPhosphorCornerStage struct {
	prev  stereoPhosphorVertex
	cur   stereoPhosphorVertex
	count int
}

func (s *stereoPhosphorCornerStage) emit(point stereoPhosphorVertex, out func(stereoPhosphorVertex)) {
	switch s.count {
	case 0:
		s.prev = point
		s.count = 1
		return
	case 1:
		out(s.prev)
		s.cur = point
		s.count = 2
		return
	}
	smoothed := s.cur
	midX := (s.prev.Position.X + point.Position.X) * 0.5
	midY := (s.prev.Position.Y + point.Position.Y) * 0.5
	smoothed.Position = stereoPhosphorPoint{
		X: s.cur.Position.X + (midX-s.cur.Position.X)*stereoPhosphorCornerSmooth,
		Y: s.cur.Position.Y + (midY-s.cur.Position.Y)*stereoPhosphorCornerSmooth,
	}
	out(smoothed)
	s.prev, s.cur = s.cur, point
}

func (s *stereoPhosphorCornerStage) flush(out func(stereoPhosphorVertex)) {
	switch s.count {
	case 1:
		out(s.prev)
	case 2:
		out(s.cur)
	}
	s.count = 0
}

func (s *stereoPhosphorState) emitCornerSmooth(point stereoPhosphorVertex) {
	if stereoPhosphorCornerPasses >= 2 {
		s.cornerA.emit(point, func(v stereoPhosphorVertex) {
			if stereoPhosphorCornerPasses >= 3 {
				s.cornerB.emit(v, func(v stereoPhosphorVertex) {
					s.cornerC.emit(v, s.emitSmooth)
				})
				return
			}
			s.cornerB.emit(v, s.emitSmooth)
		})
		return
	}
	s.cornerA.emit(point, s.emitSmooth)
}

func (s *stereoPhosphorState) flushCornerSmooth() {
	if stereoPhosphorCornerPasses >= 2 {
		s.cornerA.flush(func(v stereoPhosphorVertex) {
			if stereoPhosphorCornerPasses >= 3 {
				s.cornerB.emit(v, func(v stereoPhosphorVertex) {
					s.cornerC.emit(v, s.emitSmooth)
				})
				return
			}
			s.cornerB.emit(v, s.emitSmooth)
		})
		if stereoPhosphorCornerPasses >= 3 {
			s.cornerB.flush(func(v stereoPhosphorVertex) {
				s.cornerC.emit(v, s.emitSmooth)
			})
			s.cornerC.flush(s.emitSmooth)
		} else {
			s.cornerB.flush(s.emitSmooth)
		}
		return
	}
	s.cornerA.flush(s.emitSmooth)
}

func (s *stereoPhosphorState) emitSmooth(point stereoPhosphorVertex) {
	s.emittedControls++
	if s.smoothCount == 0 {
		s.smooth[0] = point
		s.smoothCount = 1
		s.curveValid = true
		return
	}
	if s.smoothCount == 1 {
		s.smooth[1] = point
		s.smoothCount = 2
		return
	}
	if s.smoothCount == 2 {
		s.smooth[2] = point
		s.smoothCount = 3
		s.rasterCatmullRom(s.smooth[0], s.smooth[0], s.smooth[1], s.smooth[2])
		return
	}
	s.rasterCatmullRom(s.smooth[0], s.smooth[1], s.smooth[2], point)
	s.smooth[0], s.smooth[1], s.smooth[2] = s.smooth[1], s.smooth[2], point
}

// setLinePointRate declares the collected point rate (points per second) of
// the LINE stream. The gesture low-pass derives its per-point coefficient
// from this rate, so it must reflect the real stream cadence; 0 disables the
// filter. Survives curve resets because it describes the stream, not the
// geometry chain.
func (s *stereoPhosphorState) setLinePointRate(rate float32) {
	s.linePointRate = rate
}

// lowpassLinePoint runs the mapped LINE coordinate through two cascaded
// one-pole low-pass stages. Each stage is tuned 1.5538x above the requested
// cutoff so the cascade's -3 dB point lands on stereoLineLowpassHz. The first
// point of a chain initializes both stages, so chain heads sit exactly on the
// signal instead of swinging in from a stale tail.
func (s *stereoPhosphorState) lowpassLinePoint(point stereoPhosphorPoint) stereoPhosphorPoint {
	rate := s.linePointRate
	cutoff := stereoLineLowpassHz
	if rate <= 0 || cutoff <= 0 {
		return point
	}
	if !s.lowpassValid {
		s.lowpassValid = true
		s.lowpassAX, s.lowpassAY = point.X, point.Y
		s.lowpassBX, s.lowpassBY = point.X, point.Y
		return point
	}
	alpha := float32(1 - math.Exp(-2*math.Pi*float64(cutoff)*1.5538/float64(rate)))
	s.lowpassAX += (point.X - s.lowpassAX) * alpha
	s.lowpassAY += (point.Y - s.lowpassAY) * alpha
	s.lowpassBX += (s.lowpassAX - s.lowpassBX) * alpha
	s.lowpassBY += (s.lowpassAY - s.lowpassBY) * alpha
	return stereoPhosphorPoint{X: s.lowpassBX, Y: s.lowpassBY}
}

func stereoPhosphorFilteredPoint(points [7]stereoPhosphorVertex) stereoPhosphorVertex {
	return stereoPhosphorVertex{
		Position: stereoPhosphorPoint{
			X: (points[0].Position.X + 6*points[1].Position.X + 15*points[2].Position.X + 20*points[3].Position.X + 15*points[4].Position.X + 6*points[5].Position.X + points[6].Position.X) / 64,
			Y: (points[0].Position.Y + 6*points[1].Position.Y + 15*points[2].Position.Y + 20*points[3].Position.Y + 15*points[4].Position.Y + 6*points[5].Position.Y + points[6].Position.Y) / 64,
		},
		Energy:    points[3].Energy,
		Freshness: points[3].Freshness,
	}
}

func (s *stereoPhosphorState) emitResampled(position stereoPhosphorPoint, energy, distance, freshness float32) {
	if distance < 1 {
		distance = 1
	}
	s.emitCornerSmooth(stereoPhosphorVertex{
		Position:  position,
		Freshness: freshness,
		// rasterQuadratic divides by sqrt(raster length). Compensating here
		// preserves the excitation contributed by samples merged into this span.
		Energy: energy / float32(math.Sqrt(float64(distance))),
	})
}

// Exponent applied to the per-point path length when accumulating segment
// energy. 0.5 (sqrt) is oscilloscope-faithful: fast non-repeating sweeps read
// dimmer because the same excitation is spread over a longer path. 1.0 makes
// per-pixel brightness independent of stroke speed, so drum transients flash
// as brightly as slow movement.
var stereoPhosphorSpeedEnergyExponent = float64(0.5)

func (s *stereoPhosphorState) consumeFilteredSegment(from, to stereoPhosphorVertex, spacing float32) {
	distance := stereoPhosphorDistance(from.Position, to.Position)
	segmentEnergy := to.Energy * float32(math.Pow(float64(stereoPhosphorRasterSteps(from.Position, to.Position)), stereoPhosphorSpeedEnergyExponent))
	if !s.resampleValid {
		s.resampleFrom = from
		s.resampleValid = true
		s.emitSmooth(from)
	}
	if distance < 0.0001 {
		s.resampleEnergy += segmentEnergy
		return
	}
	position := from.Position
	remaining := distance
	remainingEnergy := segmentEnergy
	for remaining > 0.0001 {
		needed := spacing - s.resampleDistance
		if needed < 0.0001 {
			// A tighter curvature-dependent spacing may become smaller than the
			// distance carried from the preceding segment. Emit at the boundary
			// instead of skipping the first dense control around the turn.
			needed = 0.0001
		}
		if remaining+0.0001 < needed {
			s.resampleDistance += remaining
			s.resampleEnergy += remainingEnergy
			break
		}
		fraction := needed / remaining
		if fraction > 1 {
			fraction = 1
		}
		position = stereoPhosphorPoint{
			X: position.X + (to.Position.X-position.X)*fraction,
			Y: position.Y + (to.Position.Y-position.Y)*fraction,
		}
		usedEnergy := remainingEnergy * fraction
		s.resampleEnergy += usedEnergy
		progress := stereoPhosphorClamp((distance-remaining+needed)/distance, 0, 1)
		freshness := from.Freshness + (to.Freshness-from.Freshness)*progress
		s.emitResampled(position, s.resampleEnergy, spacing, freshness)
		s.resampleFrom = stereoPhosphorVertex{Position: position, Energy: to.Energy, Freshness: freshness}
		s.resampleDistance = 0
		s.resampleEnergy = 0
		remaining -= needed
		remainingEnergy -= usedEnergy
	}
}

func (s *stereoPhosphorState) addFilteredPoint(point stereoPhosphorVertex, spacing float32) {
	if s.filteredCount == 0 {
		s.filteredOlder = point
		s.filteredCount = 1
		return
	}
	if s.filteredCount == 1 {
		s.filteredNewest = point
		s.filteredCount = 2
		return
	}
	adaptiveSpacing := stereoPhosphorAdaptiveSpacing(spacing, s.filteredOlder.Position, s.filteredNewest.Position, point.Position)
	s.consumeFilteredSegment(s.filteredOlder, s.filteredNewest, adaptiveSpacing)
	s.filteredOlder, s.filteredNewest = s.filteredNewest, point
}

func (s *stereoPhosphorState) addRawPoint(point stereoPhosphorVertex, spacing float32) {
	s.tailFlushed = false
	if s.rawCount == 0 {
		for i := range s.raw {
			s.raw[i] = point
		}
		s.rawCount = len(s.raw)
		s.addFilteredPoint(point, spacing)
		return
	}
	for i := 0; i < len(s.raw)-1; i++ {
		s.raw[i] = s.raw[i+1]
	}
	s.raw[len(s.raw)-1] = point
	s.addFilteredPoint(stereoPhosphorFilteredPoint(s.raw), spacing)
}

// flushCurveTail drains the finite look-ahead retained by the seven-point
// filter, spatial resampler and Catmull-Rom segment builder. It is used only
// when analysis becomes inactive and the existing image starts its fade.
func (s *stereoPhosphorState) flushCurveTail(spacing float32) bool {
	if s.tailFlushed || s.rawCount == 0 {
		return false
	}
	endpoint := s.raw[len(s.raw)-1]
	// The symmetric seven-point filter retains three samples of look-ahead.
	// Constant endpoint extension completes them without inventing a new turn.
	for i := 0; i < len(s.raw)/2; i++ {
		s.addRawPoint(endpoint, spacing)
	}
	if s.filteredCount == 2 {
		s.consumeFilteredSegment(s.filteredOlder, s.filteredNewest, spacing)
		endpoint = s.filteredNewest
	}
	if s.resampleValid {
		distance := stereoPhosphorDistance(s.resampleFrom.Position, endpoint.Position)
		if distance > 0.0001 || s.resampleEnergy > 0 {
			s.emitResampled(endpoint.Position, s.resampleEnergy, max(s.resampleDistance, distance), endpoint.Freshness)
		}
		s.resampleValid = false
		s.resampleDistance = 0
		s.resampleEnergy = 0
	}
	// Drain the corner-smoothing look-ahead. Chain tails pass through
	// unsmoothed, mirroring the unsmoothed chain heads.
	s.flushCornerSmooth()
	switch s.smoothCount {
	case 1:
		point := s.smooth[0]
		s.rasterStrokeSegment(point.Position, point.Position, point.Freshness, point.Freshness)
		s.depositEnergy(point.Position.X, point.Position.Y, point.Energy)
	case 2:
		s.rasterCatmullRom(s.smooth[0], s.smooth[0], s.smooth[1], s.smooth[1])
	case 3:
		s.rasterCatmullRom(s.smooth[0], s.smooth[1], s.smooth[2], s.smooth[2])
	}
	// A resumed analysis stream starts a new geometry chain while the already
	// rendered phosphor tail remains free to decay in its existing layers.
	s.tailFlushed = true
	return true
}

func colorChannel(color uint32, shift uint) float32 {
	return float32(byte(color>>shift)) / 255
}

func stereoPhosphorMin(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func lerpColor(from, to uint32, amount float32) uint32 {
	if amount < 0 {
		amount = 0
	} else if amount > 1 {
		amount = 1
	}
	mix := func(shift uint) byte {
		a, b := colorChannel(from, shift), colorChannel(to, shift)
		return byte(math.Round(float64((a + (b-a)*amount) * 255)))
	}
	return rgb(mix(0), mix(8), mix(16))
}

func stereoPhosphorCoreAlpha(value float32) float32 {
	if value <= 0 {
		return 0
	}
	response := float32(1 - math.Exp(-2.0*float64(value)))
	return stereoPhosphorMin(response, 1)
}

func stereoPhosphorGlowAlpha(value float32) float32 {
	if value <= 0 {
		return 0
	}
	response := float32(1-math.Exp(-12.0*float64(value))) * 0.36
	return stereoPhosphorMin(response, 0.32)
}

func stereoPhosphorFreshAlpha(value float32) float32 {
	if value <= 0 {
		return 0
	}
	return stereoPhosphorMin(value*stereoPhosphorHighlightAlpha, stereoPhosphorHighlightAlpha)
}

func stereoPhosphorGlowRadius(width, height int) int {
	radius := int(math.Round(float64(mini(width, height)) * 0.05))
	if radius < stereoPhosphorMinGlowRadius {
		return stereoPhosphorMinGlowRadius
	}
	if radius > stereoPhosphorMaxGlowRadius {
		return stereoPhosphorMaxGlowRadius
	}
	return radius
}

func stereoPhosphorCoreColor(trail, lineColor, core, peak uint32, current float32) uint32 {
	if current <= 0 {
		return trail
	}
	color := lerpColor(lineColor, core, stereoPhosphorMin(current/0.45, 1))
	if current > 0.45 {
		color = lerpColor(color, peak, stereoPhosphorMin((current-0.45)/(stereoPhosphorLimit-0.45), 1))
	}
	return color
}

func stereoPhosphorResponseIndex(value, limit float32) int {
	if value <= 0 {
		return 0
	}
	if value >= limit {
		return stereoPhosphorResponseLUTSteps
	}
	return int(value/limit*stereoPhosphorResponseLUTSteps + 0.5)
}

func (s *stereoPhosphorState) ensureColorLUT(trail, lineColor, core, peak uint32) {
	palette := [4]uint32{trail, lineColor, core, peak}
	if s.paletteValid && s.palette == palette &&
		len(s.coreColorLUT) == stereoPhosphorResponseLUTSteps+1 &&
		len(s.freshColorLUT) == stereoPhosphorResponseLUTSteps+1 {
		return
	}
	if len(s.coreColorLUT) != stereoPhosphorResponseLUTSteps+1 {
		s.coreColorLUT = make([]uint32, stereoPhosphorResponseLUTSteps+1)
	}
	if len(s.freshColorLUT) != stereoPhosphorResponseLUTSteps+1 {
		s.freshColorLUT = make([]uint32, stereoPhosphorResponseLUTSteps+1)
	}
	for i := 0; i <= stereoPhosphorResponseLUTSteps; i++ {
		current := float32(i) * stereoPhosphorLimit / stereoPhosphorResponseLUTSteps
		freshness := float32(i) / stereoPhosphorResponseLUTSteps
		s.coreColorLUT[i] = stereoPhosphorCoreColor(trail, lineColor, core, peak, current)
		s.freshColorLUT[i] = lerpColor(core, peak, freshness)
	}
	s.palette = palette
	s.paletteValid = true
}

func stereoPhosphorMax(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func stereoPhosphorOver(r, g, b, alpha *float32, color uint32, layerAlpha float32) {
	if layerAlpha <= 0 {
		return
	}
	if layerAlpha > 1 {
		layerAlpha = 1
	}
	remaining := 1 - layerAlpha
	*r = colorChannel(color, 0)*layerAlpha + *r*remaining
	*g = colorChannel(color, 8)*layerAlpha + *g*remaining
	*b = colorChannel(color, 16)*layerAlpha + *b*remaining
	*alpha = layerAlpha + *alpha*remaining
}

func (s *stereoPhosphorState) runRenderJob(job stereoPhosphorRenderJob) {
	switch job.phase {
	case stereoPhosphorPhaseComposite:
		for i := job.start; i < job.end; i++ {
			s.compositeEnergy[i] = stereoPhosphorMin(s.energyLayers[0][i]+s.energyLayers[1][i], stereoPhosphorLimit)
			s.compositeCoverage[i] = stereoPhosphorMax(s.coverageLayers[0][i], s.coverageLayers[1][i])
		}
	case stereoPhosphorPhaseBlurHorizontal1, stereoPhosphorPhaseBlurHorizontal2:
		source := s.compositeEnergy
		if job.phase == stereoPhosphorPhaseBlurHorizontal2 {
			source = s.glowEnergy
		}
		for y := job.start; y < job.end; y++ {
			row := y * s.width
			var sum float32
			for x := 0; x <= s.renderBlurRadius && x < s.width; x++ {
				sum += source[row+x]
			}
			for x := 0; x < s.width; x++ {
				s.blurScratch[row+x] = sum / float32(s.renderDiameter)
				remove, add := x-s.renderBlurRadius, x+s.renderBlurRadius+1
				if remove >= 0 {
					sum -= source[row+remove]
				}
				if add < s.width {
					sum += source[row+add]
				}
			}
		}
	case stereoPhosphorPhaseBlurVertical1:
		for x := job.start; x < job.end; x++ {
			var sum float32
			for y := 0; y <= s.renderBlurRadius && y < s.height; y++ {
				sum += s.blurScratch[y*s.width+x]
			}
			for y := 0; y < s.height; y++ {
				index := y*s.width + x
				s.glowEnergy[index] = sum / float32(s.renderDiameter)
				remove, add := y-s.renderBlurRadius, y+s.renderBlurRadius+1
				if remove >= 0 {
					sum -= s.blurScratch[remove*s.width+x]
				}
				if add < s.height {
					sum += s.blurScratch[add*s.width+x]
				}
			}
		}
	case stereoPhosphorPhaseFinalVertical:
		trail, lineColor := s.palette[0], s.palette[1]
		for x := job.start; x < job.end; x++ {
			var sum float32
			for y := 0; y <= s.renderBlurRadius && y < s.height; y++ {
				sum += s.blurScratch[y*s.width+x]
			}
			for y := 0; y < s.height; y++ {
				index := y*s.width + x
				blur := sum / float32(s.renderDiameter)
				glowAlpha := stereoPhosphorResponses.glow[stereoPhosphorResponseIndex(blur, stereoPhosphorLimit)]
				value := s.compositeEnergy[index]
				current := s.energyLayers[0][index]
				coreAlpha := stereoPhosphorResponses.core[stereoPhosphorResponseIndex(value, stereoPhosphorLimit)]
				coreColor := s.coreColorLUT[stereoPhosphorResponseIndex(current, stereoPhosphorLimit)]
				currentCoverage := s.coverageLayers[0][index]
				freshness := stereoPhosphorMax(s.highlightEnergy[index], currentCoverage)
				freshAlpha := stereoPhosphorFreshAlpha(freshness)
				freshColor := s.freshColorLUT[stereoPhosphorResponseIndex(freshness, 1)]
				baseAlpha := s.compositeCoverage[index] * stereoPhosphorBaseAlpha
				baseColor := trail
				if currentCoverage > 0 {
					baseColor = lineColor
				}
				var r, g, b, alpha float32
				stereoPhosphorOver(&r, &g, &b, &alpha, trail, glowAlpha)
				stereoPhosphorOver(&r, &g, &b, &alpha, baseColor, baseAlpha)
				stereoPhosphorOver(&r, &g, &b, &alpha, coreColor, coreAlpha)
				stereoPhosphorOver(&r, &g, &b, &alpha, freshColor, freshAlpha)
				off := index * 4
				if alpha <= 0.0001 {
					s.pixels[off+0], s.pixels[off+1], s.pixels[off+2], s.pixels[off+3] = 0, 0, 0, 0
				} else {
					s.pixels[off+0] = byte(stereoPhosphorMin(b, 1) * 255)
					s.pixels[off+1] = byte(stereoPhosphorMin(g, 1) * 255)
					s.pixels[off+2] = byte(stereoPhosphorMin(r, 1) * 255)
					s.pixels[off+3] = byte(stereoPhosphorMin(alpha, 1) * 255)
				}
				remove, add := y-s.renderBlurRadius, y+s.renderBlurRadius+1
				if remove >= 0 {
					sum -= s.blurScratch[remove*s.width+x]
				}
				if add < s.height {
					sum += s.blurScratch[add*s.width+x]
				}
			}
		}
	}
}

func (s *stereoPhosphorState) renderPixels(trail, lineColor, core, peak uint32) {
	pixelCount := s.width * s.height
	if len(s.pixels) != pixelCount*4 || len(s.highlightEnergy) != pixelCount || len(s.compositeEnergy) != pixelCount || len(s.compositeCoverage) != pixelCount || len(s.glowEnergy) != pixelCount {
		return
	}
	s.ensureColorLUT(trail, lineColor, core, peak)
	s.renderBlurRadius = stereoPhosphorGlowRadius(s.width, s.height)
	s.renderDiameter = s.renderBlurRadius*2 + 1
	if s.dc != 0 && pixelCount >= stereoPhosphorParallelPixels {
		s.ensureRenderPool()
	}
	s.runRenderPhase(stereoPhosphorPhaseComposite, pixelCount)
	s.runRenderPhase(stereoPhosphorPhaseBlurHorizontal1, s.height)
	s.runRenderPhase(stereoPhosphorPhaseBlurVertical1, s.width)
	// A second separable box pass produces the same tent-shaped CRT halo.
	s.runRenderPhase(stereoPhosphorPhaseBlurHorizontal2, s.height)
	s.runRenderPhase(stereoPhosphorPhaseFinalVertical, s.width)
}

func (s *stereoPhosphorState) advance(points []StereoPoint, sequence, tick uint64, mode, width, height int) (ready, composed bool) {
	return s.advanceStyle(points, sequence, tick, mode, 0, width, height)
}

func (s *stereoPhosphorState) advanceStyle(points []StereoPoint, sequence, tick uint64, mode, style, width, height int) (ready, composed bool) {
	if !s.ensureSurface(width, height) {
		return false, false
	}
	if s.tickValid && tick == s.lastTick {
		return true, false
	}
	elapsed := uint64(0)
	if s.tickValid && tick > s.lastTick {
		elapsed = tick - s.lastTick
	}
	s.lastTick, s.tickValid = tick, true
	if s.renderStyle != style && (s.renderStyle == 1 || style == 1) {
		s.resetDots()
	}
	s.renderStyle = style
	displayFrame := stereoLineDisplayFrame(tick)
	center := stereoPhosphorCenter(width, height)
	if style == 1 && s.dotFrameValid && displayFrame != s.dotDisplayFrame {
		// A skipped compose must still commit the completed density exposure
		// before its pixels move from the current layer into phosphor history.
		s.flushDotBins()
	}
	if style == 2 && s.rayFrameValid && displayFrame != s.rayDisplayFrame {
		// Preserve a half exposure when UI coalescing skips its nominal compose
		// tick. Deposit it before the completed frame moves into history.
		s.flushRayBins(center)
		s.rayFrameValid = false
	}
	if s.moveToDisplayFrame(displayFrame) {
		s.sequenceValid = false
	}
	if style == 0 && len(points) > 0 && s.tailFlushed {
		s.resetCurve()
	}
	selectionElapsed := elapsed
	if selectionElapsed == 0 {
		selectionElapsed = 1
	}
	initialMax := stereoPhosphorInitialMax
	switch style {
	case 1:
		initialMax = stereoPhosphorDotInitialMax
	case 2:
		initialMax = stereoPhosphorRayInitialMax
	}
	start, discontinuity := s.newPointStartLimited(len(points), sequence, initialMax)
	if discontinuity {
		if style == 0 {
			s.resetCurve()
			// Initial snapshots and true sequence gaps have no valid predecessor.
			// Limit only those catch-up paths; trimming a normal incremental batch
			// would manufacture a visible break in an otherwise continuous curve.
			if arcStart, trimmed := stereoPhosphorArcStart(points, start, mode, width, height); trimmed {
				start = arcStart
			}
		} else {
			switch style {
			case 1:
				s.resetDots()
			case 2:
				s.resetRays()
			}
		}
	}
	count := len(points) - start
	spacing := stereoPhosphorControlSpacing(width, height)
	if style == 2 && count > 0 && !s.rayFrameValid {
		s.rayDisplayFrame = displayFrame
		s.rayFrameValid = true
	}
	timeFreshness := style == 0 && s.linePointRate > 0 && stereoLineFreshWindowSec > 0
	for i := start; i < len(points); i++ {
		freshness := stereoPhosphorAgeEnergy(i-start, count)
		if timeFreshness {
			// Age in stream time keeps the freshness seam between consecutive
			// 120 Hz batches invisible: the oldest point of this batch is only
			// one tick older than the newest point of the previous one.
			pointSequence := stereoPhosphorPointSequence(sequence, len(points), i)
			var agePoints uint64
			if sequence > pointSequence {
				agePoints = sequence - pointSequence
			}
			age := float32(agePoints) / (s.linePointRate * stereoLineFreshWindowSec)
			if age > 1 {
				age = 1
			}
			freshness = 0.12 + 0.88*(1-age)*(1-age)
		}
		if style == 1 {
			pointSequence := stereoPhosphorPointSequence(sequence, len(points), i)
			mapped := stereoPhosphorMapPoint(points[i], mode, width, height)
			s.addDotPoint(mapped, freshness, pointSequence, displayFrame)
			continue
		}
		mapped := stereoPhosphorMapPoint(points[i], mode, width, height)
		switch style {
		case 2:
			s.addRayPoint(mapped, center, freshness)
		default:
			s.addRawPoint(stereoPhosphorVertex{
				Position:  s.lowpassLinePoint(mapped),
				Energy:    freshness * stereoLineTickEnergy,
				Freshness: freshness,
			}, spacing)
		}
	}
	if style == 0 && len(points) == 0 {
		s.flushCurveTail(spacing)
	}
	// If UI message coalescing skips the nominal even tick, compose the newest
	// available frame once; never replay obsolete pixel passes to catch up.
	compose := stereoLineShouldComposeFrame(tick, s.lastComposedTick, s.composedFrameValid)
	if compose && (!s.composedFrameValid || displayFrame > s.lastComposedFrame) {
		if style == 1 && s.dotFrameValid {
			s.flushDotBins()
		}
		if style == 2 && s.rayFrameValid {
			s.flushRayBins(center)
			s.rayFrameValid = false
		}
		switch {
		case s.surfaceActive:
			s.renderPixels(cStereoTrail, cStereoLine, cStereoCore, cStereoPeak)
			s.composedBlank = false
			composed = true
		case !s.composedBlank:
			// The surface holds no visible energy. Clearing the DIB directly is
			// equivalent to running the full pipeline on zeroed buffers, and the
			// single repaint it triggers blanks any previously drawn image.
			clear(s.pixels)
			s.composedBlank = true
			composed = true
		default:
			// Bitmap is already blank and on screen; skip all five pixel passes.
		}
		s.lastComposedFrame = displayFrame
		s.lastComposedTick = tick
		s.composedFrameValid = true
	}
	return true, composed
}

func (s *stereoPhosphorState) surfaceMatches(width, height int) bool {
	return s.dc != 0 && s.bitmap != 0 && s.width == width && s.height == height
}

func (s *stereoPhosphorState) draw(dc uintptr, destination RECT) bool {
	if s.dc == 0 || s.bitmap == 0 || rectW(destination) <= 0 || rectH(destination) <= 0 {
		return false
	}
	blend := uint32(AC_SRC_OVER) | uint32(255)<<16 | uint32(AC_SRC_ALPHA)<<24
	ok, _, _ := procAlphaBlend.Call(dc, uintptr(destination.Left), uintptr(destination.Top), uintptr(rectW(destination)), uintptr(rectH(destination)), s.dc, 0, 0, uintptr(s.width), uintptr(s.height), uintptr(blend))
	return ok != 0
}
