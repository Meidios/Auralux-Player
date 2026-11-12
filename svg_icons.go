//go:build windows

package main

import (
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const svgIconSupersamples = 4

type svgIconCircle struct {
	CX   string `xml:"cx,attr"`
	CY   string `xml:"cy,attr"`
	R    string `xml:"r,attr"`
	Fill string `xml:"fill,attr"`
	Mask string `xml:"mask,attr"`
}

type svgIconLine struct {
	X1             string `xml:"x1,attr"`
	Y1             string `xml:"y1,attr"`
	X2             string `xml:"x2,attr"`
	Y2             string `xml:"y2,attr"`
	Stroke         string `xml:"stroke,attr"`
	StrokeWidth    string `xml:"stroke-width,attr"`
	StrokeLinecap  string `xml:"stroke-linecap,attr"`
	StrokeLinejoin string `xml:"stroke-linejoin,attr"`
}

type svgIconPolygon struct {
	Points      string `xml:"points,attr"`
	Fill        string `xml:"fill,attr"`
	Stroke      string `xml:"stroke,attr"`
	StrokeWidth string `xml:"stroke-width,attr"`
}
type svgIconPolyline = svgIconPolygon
type svgIconPath struct {
	D           string `xml:"d,attr"`
	Fill        string `xml:"fill,attr"`
	Stroke      string `xml:"stroke,attr"`
	StrokeWidth string `xml:"stroke-width,attr"`
}
type svgIconRect struct {
	X      string `xml:"x,attr"`
	Y      string `xml:"y,attr"`
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
	Fill   string `xml:"fill,attr"`
}

type svgIconMask struct {
	ID      string          `xml:"id,attr"`
	Circles []svgIconCircle `xml:"circle"`
}

type svgIconDefs struct {
	Masks []svgIconMask `xml:"mask"`
}

type svgIconDocument struct {
	ViewBox   string            `xml:"viewBox,attr"`
	Circles   []svgIconCircle   `xml:"circle"`
	Lines     []svgIconLine     `xml:"line"`
	Polygons  []svgIconPolygon  `xml:"polygon"`
	Polylines []svgIconPolyline `xml:"polyline"`
	Rects     []svgIconRect     `xml:"rect"`
	Paths     []svgIconPath     `xml:"path"`
	Defs      svgIconDefs       `xml:"defs"`
}

type svgCircleGeometry struct {
	cx, cy, radius float64
	maskID         string
}

type svgLineGeometry struct {
	x1, y1, x2, y2, width float64
}

type svgPolylineGeometry struct {
	points []svgPoint
	width  float64
}

type svgPoint struct{ x, y float64 }

type parsedSVGIcon struct {
	minX, minY, width, height float64
	circles                   []svgCircleGeometry
	lines                     []svgLineGeometry
	polylines                 []svgPolylineGeometry
	polygons                  [][]svgPoint
	masks                     map[string][]svgCircleGeometry
}

func parseSVGPoints(value string) ([]svgPoint, error) {
	fields := strings.Fields(strings.ReplaceAll(value, ",", " "))
	if len(fields) < 6 || len(fields)%2 != 0 {
		return nil, fmt.Errorf("invalid SVG polygon points %q", value)
	}
	points := make([]svgPoint, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		x, err := svgNumber(fields[i])
		if err != nil {
			return nil, err
		}
		y, err := svgNumber(fields[i+1])
		if err != nil {
			return nil, err
		}
		points = append(points, svgPoint{x, y})
	}
	return points, nil
}

func parseSVGPath(value string) ([]svgPoint, error) {
	tokens := regexp.MustCompile(`[A-Za-z]|[-+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?`).FindAllString(value, -1)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("SVG path is empty")
	}
	points := []svgPoint{}
	var x, y float64
	command := ""
	for i := 0; i < len(tokens); {
		if strings.ContainsAny(tokens[i], "MmLlHhVvZz") {
			command = tokens[i]
			i++
			if command == "Z" || command == "z" {
				break
			}
			continue
		}
		read := func() (float64, error) {
			if i >= len(tokens) {
				return 0, fmt.Errorf("SVG path missing coordinate")
			}
			n, e := svgNumber(tokens[i])
			i++
			return n, e
		}
		switch command {
		case "M", "L", "m", "l":
			a, e := read()
			if e != nil {
				return nil, e
			}
			b, e := read()
			if e != nil {
				return nil, e
			}
			if command == "m" || command == "l" {
				a += x
				b += y
			}
			x, y = a, b
			points = append(points, svgPoint{x, y})
			if command == "M" {
				command = "L"
			} else if command == "m" {
				command = "l"
			}
		case "H", "h":
			a, e := read()
			if e != nil {
				return nil, e
			}
			if command == "h" {
				a += x
			}
			x = a
			points = append(points, svgPoint{x, y})
		case "V", "v":
			a, e := read()
			if e != nil {
				return nil, e
			}
			if command == "v" {
				a += y
			}
			y = a
			points = append(points, svgPoint{x, y})
		default:
			return nil, fmt.Errorf("unsupported SVG path command %q", command)
		}
	}
	if len(points) < 3 {
		return nil, fmt.Errorf("SVG path contains no polygon")
	}
	return points, nil
}

func svgNumber(value string) (float64, error) {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid SVG number %q", value)
	}
	return n, nil
}

func svgMaskID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "url(#") && strings.HasSuffix(value, ")") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "url(#"), ")")
	}
	return ""
}

func parseSVGCircle(value svgIconCircle) (svgCircleGeometry, error) {
	cx, err := svgNumber(value.CX)
	if err != nil {
		return svgCircleGeometry{}, err
	}
	cy, err := svgNumber(value.CY)
	if err != nil {
		return svgCircleGeometry{}, err
	}
	radius, err := svgNumber(value.R)
	if err != nil || radius <= 0 {
		return svgCircleGeometry{}, fmt.Errorf("invalid SVG circle radius %q", value.R)
	}
	return svgCircleGeometry{cx: cx, cy: cy, radius: radius, maskID: svgMaskID(value.Mask)}, nil
}

func parseSVGIcon(data []byte) (parsedSVGIcon, error) {
	var document svgIconDocument
	if err := xml.Unmarshal(data, &document); err != nil {
		return parsedSVGIcon{}, fmt.Errorf("parse SVG: %w", err)
	}
	viewBox := strings.Fields(document.ViewBox)
	if len(viewBox) != 4 {
		return parsedSVGIcon{}, fmt.Errorf("SVG viewBox must contain four numbers")
	}
	values := make([]float64, 4)
	for i := range viewBox {
		value, err := svgNumber(viewBox[i])
		if err != nil {
			return parsedSVGIcon{}, err
		}
		values[i] = value
	}
	if values[2] <= 0 || values[3] <= 0 {
		return parsedSVGIcon{}, fmt.Errorf("SVG viewBox dimensions must be positive")
	}
	icon := parsedSVGIcon{minX: values[0], minY: values[1], width: values[2], height: values[3], masks: make(map[string][]svgCircleGeometry)}
	for _, circle := range document.Circles {
		if strings.EqualFold(strings.TrimSpace(circle.Fill), "none") {
			continue
		}
		geometry, err := parseSVGCircle(circle)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		icon.circles = append(icon.circles, geometry)
	}
	for _, line := range document.Lines {
		if strings.EqualFold(strings.TrimSpace(line.Stroke), "none") {
			continue
		}
		x1, err := svgNumber(line.X1)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		y1, err := svgNumber(line.Y1)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		x2, err := svgNumber(line.X2)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		y2, err := svgNumber(line.Y2)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		width, err := svgNumber(line.StrokeWidth)
		if err != nil || width <= 0 {
			return parsedSVGIcon{}, fmt.Errorf("invalid SVG stroke width %q", line.StrokeWidth)
		}
		icon.lines = append(icon.lines, svgLineGeometry{x1: x1, y1: y1, x2: x2, y2: y2, width: width})
	}
	for _, polygon := range document.Polygons {
		if strings.EqualFold(strings.TrimSpace(polygon.Fill), "none") {
			continue
		}
		points, err := parseSVGPoints(polygon.Points)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		icon.polygons = append(icon.polygons, points)
	}
	for _, rectangle := range document.Rects {
		x, err := svgNumber(rectangle.X)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		y, err := svgNumber(rectangle.Y)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		w, err := svgNumber(rectangle.Width)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		h, err := svgNumber(rectangle.Height)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		if w <= 0 || h <= 0 {
			return parsedSVGIcon{}, fmt.Errorf("invalid SVG rect")
		}
		icon.polygons = append(icon.polygons, []svgPoint{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}})
	}
	for _, polyline := range document.Polylines {
		if strings.EqualFold(strings.TrimSpace(polyline.Stroke), "none") {
			continue
		}
		points, err := parseSVGPoints(polyline.Points)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		width, err := svgNumber(polyline.StrokeWidth)
		if err != nil || width <= 0 {
			return parsedSVGIcon{}, fmt.Errorf("invalid SVG polyline stroke width %q", polyline.StrokeWidth)
		}
		icon.polylines = append(icon.polylines, svgPolylineGeometry{points: points, width: width})
	}
	for _, path := range document.Paths {
		points, err := parseSVGPath(path.D)
		if err != nil {
			return parsedSVGIcon{}, err
		}
		if !strings.EqualFold(strings.TrimSpace(path.Fill), "none") && strings.TrimSpace(path.Fill) != "" {
			icon.polygons = append(icon.polygons, points)
		}
		if !strings.EqualFold(strings.TrimSpace(path.Stroke), "none") && strings.TrimSpace(path.Stroke) != "" {
			width, widthErr := svgNumber(path.StrokeWidth)
			if widthErr != nil || width <= 0 {
				return parsedSVGIcon{}, fmt.Errorf("invalid SVG path stroke width %q", path.StrokeWidth)
			}
			icon.polylines = append(icon.polylines, svgPolylineGeometry{points: points, width: width})
		}
	}
	for _, mask := range document.Defs.Masks {
		if strings.TrimSpace(mask.ID) == "" {
			continue
		}
		for _, circle := range mask.Circles {
			if !strings.EqualFold(strings.TrimSpace(circle.Fill), "black") {
				continue
			}
			geometry, err := parseSVGCircle(circle)
			if err != nil {
				return parsedSVGIcon{}, err
			}
			icon.masks[mask.ID] = append(icon.masks[mask.ID], geometry)
		}
	}
	if len(icon.circles) == 0 && len(icon.lines) == 0 && len(icon.polygons) == 0 && len(icon.polylines) == 0 {
		return parsedSVGIcon{}, fmt.Errorf("SVG icon contains no supported geometry")
	}
	return icon, nil
}

func pointInSVGCircle(x, y float64, circle svgCircleGeometry) bool {
	dx, dy := x-circle.cx, y-circle.cy
	return dx*dx+dy*dy <= circle.radius*circle.radius
}

func pointOnSVGLine(x, y float64, line svgLineGeometry) bool {
	dx, dy := line.x2-line.x1, line.y2-line.y1
	lengthSquared := dx*dx + dy*dy
	if lengthSquared == 0 {
		dx, dy = x-line.x1, y-line.y1
		return dx*dx+dy*dy <= line.width*line.width/4
	}
	t := ((x-line.x1)*dx + (y-line.y1)*dy) / lengthSquared
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	closestX, closestY := line.x1+t*dx, line.y1+t*dy
	dx, dy = x-closestX, y-closestY
	return dx*dx+dy*dy <= line.width*line.width/4
}

func (icon parsedSVGIcon) contains(x, y float64) bool {
	for _, circle := range icon.circles {
		if !pointInSVGCircle(x, y, circle) {
			continue
		}
		masked := false
		for _, cutout := range icon.masks[circle.maskID] {
			if pointInSVGCircle(x, y, cutout) {
				masked = true
				break
			}
		}
		if !masked {
			return true
		}
	}
	for _, line := range icon.lines {
		if pointOnSVGLine(x, y, line) {
			return true
		}
	}
	for _, polyline := range icon.polylines {
		for i := 1; i < len(polyline.points); i++ {
			p := svgLineGeometry{x1: polyline.points[i-1].x, y1: polyline.points[i-1].y, x2: polyline.points[i].x, y2: polyline.points[i].y, width: polyline.width}
			if pointOnSVGLine(x, y, p) {
				return true
			}
		}
	}
	for _, polygon := range icon.polygons {
		inside := false
		for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
			a, b := polygon[i], polygon[j]
			if ((a.y > y) != (b.y > y)) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
				inside = !inside
			}
		}
		if inside {
			return true
		}
	}
	for _, polygon := range icon.polygons {
		inside := false
		for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
			a, b := polygon[i], polygon[j]
			if ((a.y > y) != (b.y > y)) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
				inside = !inside
			}
		}
		if inside {
			return true
		}
	}
	return false
}

func rasterizeSVGIcon(data []byte, size int, tint color.NRGBA) (*image.NRGBA, error) {
	if size <= 0 {
		return nil, fmt.Errorf("SVG output size must be positive")
	}
	icon, err := parseSVGIcon(data)
	if err != nil {
		return nil, err
	}
	imageOut := image.NewNRGBA(image.Rect(0, 0, size, size))
	samples := svgIconSupersamples * svgIconSupersamples
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			covered := 0
			for sy := 0; sy < svgIconSupersamples; sy++ {
				for sx := 0; sx < svgIconSupersamples; sx++ {
					x := icon.minX + (float64(px)+(float64(sx)+0.5)/svgIconSupersamples)*icon.width/float64(size)
					y := icon.minY + (float64(py)+(float64(sy)+0.5)/svgIconSupersamples)*icon.height/float64(size)
					if icon.contains(x, y) {
						covered++
					}
				}
			}
			if covered == 0 {
				continue
			}
			alpha := uint8(math.Round(float64(tint.A) * float64(covered) / float64(samples)))
			imageOut.SetNRGBA(px, py, color.NRGBA{R: tint.R, G: tint.G, B: tint.B, A: alpha})
		}
	}
	return imageOut, nil
}

func svgThemeIconTint(name string, day bool) color.NRGBA {
	if strings.Contains(name, "sun") {
		if day {
			return color.NRGBA{R: 203, G: 146, B: 46, A: 255}
		}
		return color.NRGBA{R: 224, G: 174, B: 74, A: 255}
	}
	if day {
		return color.NRGBA{R: 57, G: 150, B: 222, A: 255}
	}
	return color.NRGBA{R: 126, G: 198, B: 248, A: 255}
}
