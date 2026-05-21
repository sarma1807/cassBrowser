package main

import (
	"bufio"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ChartPoint is a single timestamped data point.
type ChartPoint struct {
	Time  time.Time
	Value float64
}

// LineChartWidget renders a simple time-series line chart using canvas primitives.
type LineChartWidget struct {
	widget.BaseWidget
	Title     string
	Unit      string
	LineColor color.Color
	Points    []ChartPoint
}

// NewLineChartWidget creates a line chart for the given title, unit, and line colour.
func NewLineChartWidget(title, unit string, lineColor color.Color) *LineChartWidget {
	w := &LineChartWidget{Title: title, Unit: unit, LineColor: lineColor}
	w.ExtendBaseWidget(w)
	return w
}

// SetPoints replaces the chart data and redraws.
func (w *LineChartWidget) SetPoints(pts []ChartPoint) {
	w.Points = pts
	w.Refresh()
}

func (w *LineChartWidget) MinSize() fyne.Size { return fyne.NewSize(200, 240) }
func (w *LineChartWidget) CreateRenderer() fyne.WidgetRenderer {
	r := &lineChartRenderer{chart: w}
	r.build(w.MinSize())
	return r
}

// --- renderer ---

type lineChartRenderer struct {
	chart   *LineChartWidget
	objects []fyne.CanvasObject
}

// outer padding around the entire chart widget
const lcPad float32 = 12

// plot area insets (relative to the padded area)
const (
	lcTop    float32 = 30 // title
	lcLeft   float32 = 72 // y-axis labels
	lcRight  float32 = 20
	lcBottom float32 = 28 // x-axis labels
	lcInner  float32 = 8  // inner padding so data lines don't touch axis lines
)

func (r *lineChartRenderer) build(size fyne.Size) {
	objs := make([]fyne.CanvasObject, 0, 40)

	// background fills the padded inner area only
	inner := fyne.NewSize(size.Width-lcPad*2, size.Height-lcPad*2)
	bg := canvas.NewRectangle(color.RGBA{R: 30, G: 30, B: 30, A: 40})
	bg.StrokeColor = color.RGBA{R: 100, G: 100, B: 100, A: 80}
	bg.StrokeWidth = 1
	bg.Resize(inner)
	bg.Move(fyne.NewPos(lcPad, lcPad))
	objs = append(objs, bg)

	title := canvas.NewText(r.chart.Title, theme.ForegroundColor())
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.TextSize = theme.TextSize()
	title.Move(fyne.NewPos(lcPad+lcLeft, lcPad+3))
	objs = append(objs, title)

	pX := lcPad + lcLeft
	pY := lcPad + lcTop
	pW := size.Width - pX - lcRight - lcPad
	pH := size.Height - pY - lcBottom - lcPad

	if pW < 20 || pH < 20 {
		r.objects = objs
		return
	}

	axisCol := color.RGBA{R: 110, G: 110, B: 110, A: 200}

	lAxis := canvas.NewLine(axisCol)
	lAxis.StrokeWidth = 1
	lAxis.Position1 = fyne.NewPos(pX, pY)
	lAxis.Position2 = fyne.NewPos(pX, pY+pH)
	objs = append(objs, lAxis)

	bAxis := canvas.NewLine(axisCol)
	bAxis.StrokeWidth = 1
	bAxis.Position1 = fyne.NewPos(pX, pY+pH)
	bAxis.Position2 = fyne.NewPos(pX+pW, pY+pH)
	objs = append(objs, bAxis)

	// data area is inset from the axis lines
	pX += lcInner
	pY += lcInner
	pW -= lcInner * 2
	pH -= lcInner * 2

	pts := r.chart.Points
	if len(pts) < 2 {
		msg := "no data"
		if len(pts) == 1 {
			msg = "only 1 point"
		}
		nd := canvas.NewText(msg, color.RGBA{R: 150, G: 150, B: 150, A: 200})
		nd.TextSize = theme.TextSize() - 2
		nd.Move(fyne.NewPos(pX+pW/2-24, pY+pH/2-8))
		objs = append(objs, nd)
		r.objects = objs
		return
	}

	minV, maxV := pts[0].Value, pts[0].Value
	for _, p := range pts {
		if p.Value < minV {
			minV = p.Value
		}
		if p.Value > maxV {
			maxV = p.Value
		}
	}
	vRange := maxV - minV
	if vRange == 0 {
		vRange = 1
	}

	labelCol := theme.ForegroundColor()
	smallSz := theme.TextSize() - 1

	const ySteps = 4
	for i := 0; i <= ySteps; i++ {
		frac := float32(i) / float32(ySteps)
		val := maxV - float64(frac)*vRange
		yPos := pY + frac*pH
		lbl := canvas.NewText(r.fmtValue(val), labelCol)
		lbl.TextSize = smallSz
		lbl.Move(fyne.NewPos(lcPad+2, yPos-6))
		objs = append(objs, lbl)
		if i > 0 && i < ySteps {
			guide := canvas.NewLine(color.RGBA{R: 80, G: 80, B: 80, A: 100})
			guide.StrokeWidth = 1
			guide.Position1 = fyne.NewPos(pX, yPos)
			guide.Position2 = fyne.NewPos(pX+pW, yPos)
			objs = append(objs, guide)
		}
	}

	minT := pts[0].Time.UnixNano()
	maxT := pts[len(pts)-1].Time.UnixNano()
	tRange := float64(maxT - minT)
	if tRange == 0 {
		tRange = 1
	}

	const xSteps = 5
	for i := 0; i <= xSteps; i++ {
		frac := float64(i) / float64(xSteps)
		t := pts[0].Time.Add(time.Duration(frac * float64(pts[len(pts)-1].Time.Sub(pts[0].Time))))
		xPos := pX + float32(frac)*pW
		lbl := canvas.NewText(t.Format("15:04"), labelCol)
		lbl.TextSize = smallSz
		var xOff float32
		if i == xSteps {
			xOff = 26
		} else if i == 0 {
			xOff = 0
		} else {
			xOff = 13
		}
		lbl.Move(fyne.NewPos(xPos-xOff, size.Height-lcPad-14))
		objs = append(objs, lbl)
	}

	toPos := func(p ChartPoint) fyne.Position {
		nx := float32(float64(p.Time.UnixNano()-minT) / tRange)
		ny := float32(1.0 - (p.Value-minV)/vRange)
		return fyne.NewPos(pX+nx*pW, pY+ny*pH)
	}

	for i := 1; i < len(pts); i++ {
		ln := canvas.NewLine(r.chart.LineColor)
		ln.StrokeWidth = 1.5
		ln.Position1 = toPos(pts[i-1])
		ln.Position2 = toPos(pts[i])
		objs = append(objs, ln)
	}

	r.objects = objs
}

func (r *lineChartRenderer) fmtValue(v float64) string {
	switch r.chart.Unit {
	case "bytes":
		return fmtBytes(v)
	case "%":
		return fmt.Sprintf("%.1f%%", v)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

func (r *lineChartRenderer) Layout(size fyne.Size) { r.build(size) }
func (r *lineChartRenderer) MinSize() fyne.Size    { return fyne.NewSize(200, 240) }
func (r *lineChartRenderer) Refresh() {
	sz := r.chart.Size()
	if sz.Width < 1 {
		sz = r.MinSize()
	}
	r.build(sz)
	canvas.Refresh(r.chart)
}
func (r *lineChartRenderer) Destroy()                     {}
func (r *lineChartRenderer) Objects() []fyne.CanvasObject { return r.objects }

// fmtBytes formats a byte count as a human-readable string.
func fmtBytes(b float64) string {
	if b < 1024 {
		return fmt.Sprintf("%.0fB", b)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	val := b / 1024
	for _, u := range units {
		if val < 1024 {
			return fmt.Sprintf("%.1f%s", val, u)
		}
		val /= 1024
	}
	return fmt.Sprintf("%.1fPB", val)
}

// --- stats file parsing ---

type statsData struct {
	am, ac, dm []ChartPoint
}

// loadRecentStats reads the past 2 days of stats log files from workDir and
// returns combined parsed points in chronological order.
func loadRecentStats(workDir string) statsData {
	now := time.Now()
	var sd statsData
	for _, d := range []time.Time{now.AddDate(0, 0, -1), now} {
		parseStatsFile(workDir, d, &sd)
	}
	return sd
}

func parseStatsFile(workDir string, day time.Time, sd *statsData) {
	logPath := filepath.Join(workDir, logDirName, fmt.Sprintf("%s_%s.log", statsFilePrefix, day.Format("20060102")))

	f, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " | ", 2)
		if len(parts) != 2 {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", parts[0], time.Local)
		if err != nil {
			continue
		}
		kv := strings.SplitN(parts[1], "=", 2)
		if len(kv) != 2 {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err != nil {
			continue
		}
		pt := ChartPoint{Time: t, Value: val}
		switch strings.TrimSpace(kv[0]) {
		case "am":
			sd.am = append(sd.am, pt)
		case "ac":
			sd.ac = append(sd.ac, pt)
		case "dm":
			sd.dm = append(sd.dm, pt)
		}
	}
}
