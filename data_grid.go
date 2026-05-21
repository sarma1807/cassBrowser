package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	defaultRowNumWidth float32 = 40
	defaultColWidth    float32 = 150
	minColWidth        float32 = 30
	maxAutoColWidth    float32 = 300 // cap for auto-sizing (~36 chars at default font)
)

// DataGrid represents a paginated data table.
type DataGrid struct {
	columns       []string
	rows          []map[string]interface{}
	rowsPerPage   int
	currentPage   int
	totalPages    int
	container     *fyne.Container
	table         *widget.Table
	pageLabel     *widget.Label
	prevBtn       *widget.Button
	nextBtn       *widget.Button
	paginationBox *fyne.Container
	state         *AppState
	columnWidths  []float32 // index 0 = row-number col, 1..n = data cols
}

// NewDataGrid creates a new data grid
func NewDataGrid(columns []string, rows []map[string]interface{}, rowsPerPage int, state *AppState) *DataGrid {
	if rowsPerPage <= 0 {
		rowsPerPage = 10
	}

	dg := &DataGrid{
		columns:     columns,
		rows:        rows,
		rowsPerPage: rowsPerPage,
		currentPage: 0,
		state:       state,
	}

	dg.columnWidths = make([]float32, len(columns)+1)
	dg.calculateAutoColumnWidths()
	dg.calculatePages()
	dg.createUI()
	return dg
}

func (dg *DataGrid) calculatePages() {
	if len(dg.rows) == 0 {
		dg.totalPages = 1
		return
	}
	dg.totalPages = (len(dg.rows) + dg.rowsPerPage - 1) / dg.rowsPerPage
}

// calculateAutoColumnWidths sizes each column to fit its widest content,
// capped at maxAutoColWidth so long-text columns stay readable.
func (dg *DataGrid) calculateAutoColumnWidths() {
	_, fontSize, _, _ := dg.state.GetGridFontSettings()
	if fontSize <= 0 {
		fontSize = 14
	}
	charW := fontSize * 0.58
	cellPad := float32(8) // horizontal cell padding

	// Row-number column: wide enough for the largest row number.
	digits := len(fmt.Sprintf("%d", len(dg.rows)))
	if digits < 1 {
		digits = 1
	}
	rnW := float32(digits)*charW + cellPad + float32(0)
	if rnW < defaultRowNumWidth {
		rnW = defaultRowNumWidth
	}
	dg.columnWidths[0] = rnW

	// Data columns: widest of header text vs sampled row values, capped.
	// Sampling the first 200 rows avoids O(n×m) cost on large result sets.
	sample := dg.rows[:min(len(dg.rows), 200)]
	for i, colName := range dg.columns {
		// Header text (plus handle) sets the lower bound.
		w := float32(len([]rune(colName)))*charW + cellPad + float32(0)

		for _, row := range sample {
			cellW := float32(len([]rune(formatValue(row[colName]))))*charW + cellPad
			if cellW > w {
				w = cellW
			}
		}

		if w > maxAutoColWidth {
			w = maxAutoColWidth
		}
		if w < minColWidth {
			w = minColWidth
		}
		dg.columnWidths[i+1] = w
	}
}

func (dg *DataGrid) createUI() {
	dg.table = widget.NewTable(
		dg.tableLength,
		dg.createCell,
		dg.updateCell,
	)

	for i, w := range dg.columnWidths {
		dg.table.SetColumnWidth(i, w)
	}

	dg.prevBtn = widget.NewButton("<< Prev", dg.prevPage)
	dg.nextBtn = widget.NewButton("Next >>", dg.nextPage)
	dg.pageLabel = widget.NewLabel(dg.getPageLabel())

	dg.paginationBox = container.NewHBox(dg.prevBtn, dg.pageLabel, dg.nextBtn)
	dg.updatePaginationButtons()

	dg.container = container.NewBorder(dg.paginationBox, nil, nil, nil, dg.table)
}

// Container returns the grid container
func (dg *DataGrid) Container() fyne.CanvasObject {
	return dg.container
}

// SetCappedNote appends an italic note label to the pagination row.
func (dg *DataGrid) SetCappedNote(msg string) {
	note := widget.NewLabel(msg)
	note.TextStyle = fyne.TextStyle{Italic: true}
	dg.paginationBox.Add(note)
	dg.paginationBox.Refresh()
}

func (dg *DataGrid) tableLength() (int, int) {
	return dg.getVisibleRowCount() + 1, len(dg.columns) + 1 // +1 row for header
}

func (dg *DataGrid) getVisibleRowCount() int {
	startIdx := dg.currentPage * dg.rowsPerPage
	endIdx := startIdx + dg.rowsPerPage
	if endIdx > len(dg.rows) {
		endIdx = len(dg.rows)
	}
	return endIdx - startIdx
}

func (dg *DataGrid) createCell() fyne.CanvasObject {
	_, fontSize, _, _ := dg.state.GetGridFontSettings()
	if fontSize <= 0 {
		fontSize = 14
	}
	text := canvas.NewText("Template", theme.ForegroundColor())
	text.TextSize = fontSize
	text.TextStyle = dg.getGridTextStyle(false)
	return text
}

func (dg *DataGrid) updateCell(id widget.TableCellID, obj fyne.CanvasObject) {
	text := obj.(*canvas.Text)

	_, fontSize, _, _ := dg.state.GetGridFontSettings()
	if fontSize <= 0 {
		fontSize = 14
	}
	charW := fontSize * 0.58
	text.TextSize = fontSize
	text.Color = theme.ForegroundColor()

	colWidth := float32(0)
	if id.Col < len(dg.columnWidths) {
		colWidth = dg.columnWidths[id.Col]
	}

	if id.Row == 0 {
		text.TextStyle = dg.getGridTextStyle(true)
		var rawText string
		if id.Col == 0 {
			rawText = "#"
		} else if id.Col-1 < len(dg.columns) {
			rawText = dg.columns[id.Col-1]
		}
		text.Text = truncateToWidth(rawText, colWidth, charW)
		text.Refresh()
		return
	}

	text.TextStyle = dg.getGridTextStyle(false)
	rowIdx := dg.currentPage*dg.rowsPerPage + (id.Row - 1)
	if rowIdx >= len(dg.rows) {
		text.Text = ""
		text.Refresh()
		return
	}

	row := dg.rows[rowIdx]
	var rawText string
	if id.Col == 0 {
		rawText = fmt.Sprintf("%d", rowIdx+1)
	} else if id.Col-1 < len(dg.columns) {
		colName := dg.columns[id.Col-1]
		rawText = formatValue(row[colName])
	}

	text.Text = truncateToWidth(rawText, colWidth, charW)
	text.Refresh()
}

// truncateToWidth clips s to fit within width pixels, appending "…" when
// truncated. charW is the pre-computed per-character pixel width (fontSize*0.58).
func truncateToWidth(s string, width, charW float32) string {
	if width <= 0 || charW <= 0 {
		return s
	}
	maxChars := int((width - 8) / charW) // subtract cell horizontal padding
	if maxChars < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-3]) + "..."
}

func (dg *DataGrid) getGridTextStyle(bold bool) fyne.TextStyle {
	style := fyne.TextStyle{}
	if dg.state != nil {
		fontName, _, fontBold, fontItalic := dg.state.GetGridFontSettings()
		style.Monospace = fontName == "monospace"
		style.Bold = fontBold || bold
		style.Italic = fontItalic
	} else {
		style.Bold = bold
	}
	return style
}

// formatValue formats a value for display
func formatValue(v interface{}) string {
	if v == nil {
		return "<null>"
	}
	return fmt.Sprintf("%v", v)
}

func (dg *DataGrid) getPageLabel() string {
	return fmt.Sprintf("Page %d of %s (%s rows)", dg.currentPage+1, commaInt(dg.totalPages), commaInt(len(dg.rows)))
}

func (dg *DataGrid) updatePaginationButtons() {
	dg.prevBtn.Disable()
	dg.nextBtn.Disable()
	if dg.currentPage > 0 {
		dg.prevBtn.Enable()
	}
	if dg.currentPage < dg.totalPages-1 {
		dg.nextBtn.Enable()
	}
	dg.pageLabel.SetText(dg.getPageLabel())
}

func (dg *DataGrid) prevPage() {
	if dg.currentPage > 0 {
		dg.currentPage--
		dg.table.Refresh()
		dg.updatePaginationButtons()
	}
}

func (dg *DataGrid) nextPage() {
	if dg.currentPage < dg.totalPages-1 {
		dg.currentPage++
		dg.table.Refresh()
		dg.updatePaginationButtons()
	}
}

// Refresh refreshes the grid
func (dg *DataGrid) Refresh() {
	dg.table.Refresh()
	dg.updatePaginationButtons()
}
