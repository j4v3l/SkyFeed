package track

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"time"
)

const plotSize = 640

func (store *Store) Plot(icao string) ([]byte, Summary, error) {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	now := store.clock().UTC()
	store.mu.Lock()
	store.pruneLocked(now)
	recordValue := store.records[icao]
	if recordValue == nil || len(recordValue.points) == 0 {
		store.mu.Unlock()
		return nil, Summary{}, ErrNotFound
	}
	lastSample := recordValue.points[len(recordValue.points)-1].At
	if len(recordValue.plot) > 0 && now.Sub(recordValue.plotAt) < store.plotTTL && recordValue.plotKey.Equal(lastSample) {
		data := append([]byte(nil), recordValue.plot...)
		points := append([]Point(nil), recordValue.points...)
		store.mu.Unlock()
		return data, summarize(icao, points), nil
	}
	points := append([]Point(nil), recordValue.points...)
	store.mu.Unlock()

	data, err := renderPlot(points)
	if err != nil {
		return nil, Summary{}, err
	}
	store.mu.Lock()
	if current := store.records[icao]; current != nil && len(current.points) > 0 && current.points[len(current.points)-1].At.Equal(lastSample) {
		store.evictPlotLocked()
		current.plot = append(current.plot[:0], data...)
		current.plotAt = now
		current.plotKey = lastSample
	}
	store.mu.Unlock()
	return data, summarize(icao, points), nil
}

func (store *Store) evictPlotLocked() {
	plotCount := 0
	var oldest *record
	for _, candidate := range store.records {
		if len(candidate.plot) == 0 {
			continue
		}
		plotCount++
		if oldest == nil || candidate.plotAt.Before(oldest.plotAt) {
			oldest = candidate
		}
	}
	if plotCount >= defaultMaxPlots && oldest != nil {
		oldest.plot = nil
		oldest.plotAt = time.Time{}
		oldest.plotKey = time.Time{}
	}
}

func renderPlot(points []Point) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, plotSize, plotSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 7, G: 15, B: 24, A: 255}}, image.Point{}, draw.Src)
	center := plotSize / 2
	radar := color.RGBA{R: 53, G: 208, B: 127, A: 100}
	for _, radius := range []int{80, 160, 240, 300} {
		drawCircle(canvas, center, center, radius, radar)
	}
	drawLine(canvas, center, 16, center, plotSize-16, radar)
	drawLine(canvas, 16, center, plotSize-16, center, radar)
	trackColor := color.RGBA{R: 55, G: 181, B: 255, A: 255}
	previousX, previousY := 0, 0
	coordinates := plotCoordinates(points, center)
	for index, coordinate := range coordinates {
		x, y := coordinate.X, coordinate.Y
		if index > 0 {
			drawLine(canvas, previousX, previousY, x, y, trackColor)
		}
		previousX, previousY = x, y
	}
	drawDot(canvas, previousX, previousY, 5, color.RGBA{R: 243, G: 182, B: 58, A: 255})
	var buffer bytes.Buffer
	err := png.Encode(&buffer, canvas)
	return buffer.Bytes(), err
}

func plotCoordinates(points []Point, center int) []image.Point {
	allPositioned := len(points) >= 2
	for _, point := range points {
		allPositioned = allPositioned && point.HasPosition
	}
	if allPositioned {
		return geographicCoordinates(points, center)
	}
	maxDistance := 1.0
	for _, point := range points {
		maxDistance = max(maxDistance, point.DistanceNM)
	}
	maxDistance *= 1.1
	result := make([]image.Point, 0, len(points))
	for _, point := range points {
		radius := point.DistanceNM / maxDistance * 292
		angle := point.BearingDegrees * math.Pi / 180
		result = append(result, image.Pt(center+int(math.Sin(angle)*radius), center-int(math.Cos(angle)*radius)))
	}
	return result
}

func geographicCoordinates(points []Point, center int) []image.Point {
	meanLatitude := 0.0
	for _, point := range points {
		meanLatitude += point.Latitude
	}
	meanLatitude /= float64(len(points))
	longitudeScale := math.Cos(meanLatitude * math.Pi / 180)
	if math.Abs(longitudeScale) < 0.01 {
		longitudeScale = 0.01
	}
	minX, maxX := points[0].Longitude*longitudeScale, points[0].Longitude*longitudeScale
	minY, maxY := points[0].Latitude, points[0].Latitude
	for _, point := range points[1:] {
		x := point.Longitude * longitudeScale
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, point.Latitude), max(maxY, point.Latitude)
	}
	span := max(maxX-minX, maxY-minY)
	if span < 0.0001 {
		span = 0.0001
	}
	midX, midY := (minX+maxX)/2, (minY+maxY)/2
	scale := 560.0 / span
	result := make([]image.Point, 0, len(points))
	for _, point := range points {
		x := center + int((point.Longitude*longitudeScale-midX)*scale)
		y := center - int((point.Latitude-midY)*scale)
		result = append(result, image.Pt(x, y))
	}
	return result
}

func drawCircle(target *image.RGBA, centerX, centerY, radius int, value color.RGBA) {
	for degrees := 0; degrees < 360; degrees++ {
		angle := float64(degrees) * math.Pi / 180
		x := centerX + int(math.Cos(angle)*float64(radius))
		y := centerY + int(math.Sin(angle)*float64(radius))
		target.SetRGBA(x, y, value)
	}
}

func drawLine(target *image.RGBA, x0, y0, x1, y1 int, value color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(target.Bounds()) {
			target.SetRGBA(x0, y0, value)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		twice := 2 * err
		if twice >= dy {
			err += dy
			x0 += sx
		}
		if twice <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawDot(target *image.RGBA, centerX, centerY, radius int, value color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				target.SetRGBA(centerX+x, centerY+y, value)
			}
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
