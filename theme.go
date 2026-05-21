package main

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// ThemeScheme represents a complete color scheme
type ThemeScheme struct {
	Name           string
	Mode           string // "dark" or "light"
	Primary        color.Color
	Background     color.Color
	Foreground     color.Color
	Button         color.Color
	ButtonDisabled color.Color
	Input          color.Color
	Menu           color.Color
	Separator      color.Color
	Selection      color.Color
	Hover          color.Color
	ScrollBar      color.Color
	Placeholder    color.Color
}

// DarkThemes contains dark mode color schemes (9 themes)
var DarkThemes = []ThemeScheme{
	{
		Name:           "Midnight Gold",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 221, G: 176, B: 56, A: 255},
		Background:     color.NRGBA{R: 25, G: 25, B: 30, A: 255},
		Foreground:     color.NRGBA{R: 230, G: 230, B: 230, A: 255},
		Button:         color.NRGBA{R: 45, G: 45, B: 50, A: 255},
		ButtonDisabled: color.NRGBA{R: 35, G: 35, B: 40, A: 255},
		Input:          color.NRGBA{R: 35, G: 35, B: 40, A: 255},
		Menu:           color.NRGBA{R: 30, G: 30, B: 35, A: 255},
		Separator:      color.NRGBA{R: 60, G: 60, B: 65, A: 255},
		Selection:      color.NRGBA{R: 100, G: 90, B: 50, A: 100},
		Hover:          color.NRGBA{R: 60, G: 55, B: 40, A: 100},
		ScrollBar:      color.NRGBA{R: 80, G: 75, B: 60, A: 255},
		Placeholder:    color.NRGBA{R: 128, G: 128, B: 128, A: 255},
	},
	{
		Name:           "Ocean Blue",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 70, G: 150, B: 220, A: 255},
		Background:     color.NRGBA{R: 20, G: 25, B: 35, A: 255},
		Foreground:     color.NRGBA{R: 220, G: 230, B: 240, A: 255},
		Button:         color.NRGBA{R: 35, G: 45, B: 60, A: 255},
		ButtonDisabled: color.NRGBA{R: 30, G: 35, B: 45, A: 255},
		Input:          color.NRGBA{R: 30, G: 40, B: 55, A: 255},
		Menu:           color.NRGBA{R: 25, G: 30, B: 40, A: 255},
		Separator:      color.NRGBA{R: 50, G: 65, B: 85, A: 255},
		Selection:      color.NRGBA{R: 50, G: 90, B: 130, A: 100},
		Hover:          color.NRGBA{R: 40, G: 70, B: 100, A: 100},
		ScrollBar:      color.NRGBA{R: 60, G: 90, B: 120, A: 255},
		Placeholder:    color.NRGBA{R: 120, G: 140, B: 160, A: 255},
	},
	{
		Name:           "Forest Green",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 80, G: 180, B: 100, A: 255},
		Background:     color.NRGBA{R: 20, G: 28, B: 22, A: 255},
		Foreground:     color.NRGBA{R: 220, G: 235, B: 225, A: 255},
		Button:         color.NRGBA{R: 35, G: 50, B: 40, A: 255},
		ButtonDisabled: color.NRGBA{R: 28, G: 38, B: 32, A: 255},
		Input:          color.NRGBA{R: 30, G: 45, B: 35, A: 255},
		Menu:           color.NRGBA{R: 25, G: 35, B: 28, A: 255},
		Separator:      color.NRGBA{R: 50, G: 75, B: 55, A: 255},
		Selection:      color.NRGBA{R: 50, G: 100, B: 60, A: 100},
		Hover:          color.NRGBA{R: 45, G: 80, B: 50, A: 100},
		ScrollBar:      color.NRGBA{R: 60, G: 100, B: 70, A: 255},
		Placeholder:    color.NRGBA{R: 120, G: 150, B: 130, A: 255},
	},
	{
		Name:           "Purple Haze",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 180, G: 100, B: 220, A: 255},
		Background:     color.NRGBA{R: 25, G: 20, B: 30, A: 255},
		Foreground:     color.NRGBA{R: 235, G: 225, B: 240, A: 255},
		Button:         color.NRGBA{R: 45, G: 35, B: 55, A: 255},
		ButtonDisabled: color.NRGBA{R: 35, G: 28, B: 42, A: 255},
		Input:          color.NRGBA{R: 40, G: 32, B: 50, A: 255},
		Menu:           color.NRGBA{R: 30, G: 25, B: 38, A: 255},
		Separator:      color.NRGBA{R: 70, G: 55, B: 85, A: 255},
		Selection:      color.NRGBA{R: 100, G: 60, B: 120, A: 100},
		Hover:          color.NRGBA{R: 80, G: 50, B: 100, A: 100},
		ScrollBar:      color.NRGBA{R: 100, G: 70, B: 120, A: 255},
		Placeholder:    color.NRGBA{R: 140, G: 120, B: 155, A: 255},
	},
	{
		Name:           "Crimson Night",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 220, G: 60, B: 80, A: 255},
		Background:     color.NRGBA{R: 28, G: 20, B: 22, A: 255},
		Foreground:     color.NRGBA{R: 240, G: 230, B: 232, A: 255},
		Button:         color.NRGBA{R: 55, G: 35, B: 40, A: 255},
		ButtonDisabled: color.NRGBA{R: 42, G: 28, B: 32, A: 255},
		Input:          color.NRGBA{R: 45, G: 30, B: 35, A: 255},
		Menu:           color.NRGBA{R: 35, G: 25, B: 28, A: 255},
		Separator:      color.NRGBA{R: 80, G: 50, B: 55, A: 255},
		Selection:      color.NRGBA{R: 120, G: 50, B: 60, A: 100},
		Hover:          color.NRGBA{R: 100, G: 40, B: 50, A: 100},
		ScrollBar:      color.NRGBA{R: 120, G: 60, B: 70, A: 255},
		Placeholder:    color.NRGBA{R: 160, G: 120, B: 125, A: 255},
	},
	{
		Name:           "Teal Shadow",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 0, G: 180, B: 170, A: 255},
		Background:     color.NRGBA{R: 18, G: 28, B: 28, A: 255},
		Foreground:     color.NRGBA{R: 220, G: 240, B: 238, A: 255},
		Button:         color.NRGBA{R: 30, G: 50, B: 50, A: 255},
		ButtonDisabled: color.NRGBA{R: 25, G: 40, B: 40, A: 255},
		Input:          color.NRGBA{R: 25, G: 45, B: 45, A: 255},
		Menu:           color.NRGBA{R: 22, G: 35, B: 35, A: 255},
		Separator:      color.NRGBA{R: 40, G: 75, B: 75, A: 255},
		Selection:      color.NRGBA{R: 30, G: 100, B: 95, A: 100},
		Hover:          color.NRGBA{R: 25, G: 80, B: 78, A: 100},
		ScrollBar:      color.NRGBA{R: 50, G: 100, B: 98, A: 255},
		Placeholder:    color.NRGBA{R: 110, G: 150, B: 148, A: 255},
	},
	{
		Name:           "Copper Rust",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 200, G: 120, B: 70, A: 255},
		Background:     color.NRGBA{R: 30, G: 24, B: 20, A: 255},
		Foreground:     color.NRGBA{R: 240, G: 232, B: 225, A: 255},
		Button:         color.NRGBA{R: 55, G: 45, B: 38, A: 255},
		ButtonDisabled: color.NRGBA{R: 42, G: 35, B: 30, A: 255},
		Input:          color.NRGBA{R: 48, G: 40, B: 34, A: 255},
		Menu:           color.NRGBA{R: 38, G: 30, B: 25, A: 255},
		Separator:      color.NRGBA{R: 85, G: 65, B: 50, A: 255},
		Selection:      color.NRGBA{R: 110, G: 70, B: 45, A: 100},
		Hover:          color.NRGBA{R: 90, G: 60, B: 40, A: 100},
		ScrollBar:      color.NRGBA{R: 120, G: 85, B: 60, A: 255},
		Placeholder:    color.NRGBA{R: 160, G: 140, B: 125, A: 255},
	},
	{
		Name:           "Slate Gray",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 140, G: 160, B: 180, A: 255},
		Background:     color.NRGBA{R: 30, G: 32, B: 36, A: 255},
		Foreground:     color.NRGBA{R: 230, G: 235, B: 240, A: 255},
		Button:         color.NRGBA{R: 50, G: 54, B: 60, A: 255},
		ButtonDisabled: color.NRGBA{R: 40, G: 43, B: 48, A: 255},
		Input:          color.NRGBA{R: 42, G: 46, B: 52, A: 255},
		Menu:           color.NRGBA{R: 35, G: 38, B: 42, A: 255},
		Separator:      color.NRGBA{R: 70, G: 76, B: 85, A: 255},
		Selection:      color.NRGBA{R: 80, G: 90, B: 105, A: 100},
		Hover:          color.NRGBA{R: 65, G: 72, B: 82, A: 100},
		ScrollBar:      color.NRGBA{R: 90, G: 100, B: 115, A: 255},
		Placeholder:    color.NRGBA{R: 130, G: 140, B: 155, A: 255},
	},
	{
		Name:           "Neon Pink",
		Mode:           "dark",
		Primary:        color.NRGBA{R: 255, G: 80, B: 180, A: 255},
		Background:     color.NRGBA{R: 25, G: 18, B: 25, A: 255},
		Foreground:     color.NRGBA{R: 245, G: 235, B: 242, A: 255},
		Button:         color.NRGBA{R: 50, G: 35, B: 48, A: 255},
		ButtonDisabled: color.NRGBA{R: 40, G: 28, B: 38, A: 255},
		Input:          color.NRGBA{R: 45, G: 32, B: 42, A: 255},
		Menu:           color.NRGBA{R: 32, G: 22, B: 30, A: 255},
		Separator:      color.NRGBA{R: 80, G: 50, B: 72, A: 255},
		Selection:      color.NRGBA{R: 140, G: 50, B: 100, A: 100},
		Hover:          color.NRGBA{R: 110, G: 45, B: 85, A: 100},
		ScrollBar:      color.NRGBA{R: 140, G: 60, B: 110, A: 255},
		Placeholder:    color.NRGBA{R: 170, G: 130, B: 155, A: 255},
	},
}

// LightThemes contains light mode color schemes (9 themes)
var LightThemes = []ThemeScheme{
	{
		Name:           "Classic Light",
		Mode:           "light",
		Primary:        color.NRGBA{R: 50, G: 100, B: 180, A: 255},
		Background:     color.NRGBA{R: 248, G: 248, B: 250, A: 255},
		Foreground:     color.NRGBA{R: 30, G: 30, B: 35, A: 255},
		Button:         color.NRGBA{R: 230, G: 232, B: 236, A: 255},
		ButtonDisabled: color.NRGBA{R: 210, G: 212, B: 216, A: 255},
		Input:          color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Menu:           color.NRGBA{R: 245, G: 245, B: 248, A: 255},
		Separator:      color.NRGBA{R: 200, G: 200, B: 205, A: 255},
		Selection:      color.NRGBA{R: 180, G: 210, B: 255, A: 100},
		Hover:          color.NRGBA{R: 220, G: 235, B: 255, A: 100},
		ScrollBar:      color.NRGBA{R: 180, G: 185, B: 195, A: 255},
		Placeholder:    color.NRGBA{R: 140, G: 140, B: 150, A: 255},
	},
	{
		Name:           "Warm Cream",
		Mode:           "light",
		Primary:        color.NRGBA{R: 180, G: 120, B: 60, A: 255},
		Background:     color.NRGBA{R: 255, G: 252, B: 245, A: 255},
		Foreground:     color.NRGBA{R: 60, G: 50, B: 40, A: 255},
		Button:         color.NRGBA{R: 245, G: 238, B: 225, A: 255},
		ButtonDisabled: color.NRGBA{R: 230, G: 225, B: 215, A: 255},
		Input:          color.NRGBA{R: 255, G: 255, B: 252, A: 255},
		Menu:           color.NRGBA{R: 250, G: 245, B: 235, A: 255},
		Separator:      color.NRGBA{R: 220, G: 210, B: 195, A: 255},
		Selection:      color.NRGBA{R: 240, G: 220, B: 180, A: 100},
		Hover:          color.NRGBA{R: 250, G: 240, B: 220, A: 100},
		ScrollBar:      color.NRGBA{R: 200, G: 190, B: 170, A: 255},
		Placeholder:    color.NRGBA{R: 160, G: 150, B: 130, A: 255},
	},
	{
		Name:           "Mint Fresh",
		Mode:           "light",
		Primary:        color.NRGBA{R: 40, G: 160, B: 120, A: 255},
		Background:     color.NRGBA{R: 245, G: 252, B: 250, A: 255},
		Foreground:     color.NRGBA{R: 30, G: 45, B: 40, A: 255},
		Button:         color.NRGBA{R: 225, G: 242, B: 238, A: 255},
		ButtonDisabled: color.NRGBA{R: 210, G: 228, B: 224, A: 255},
		Input:          color.NRGBA{R: 252, G: 255, B: 254, A: 255},
		Menu:           color.NRGBA{R: 238, G: 248, B: 245, A: 255},
		Separator:      color.NRGBA{R: 190, G: 215, B: 208, A: 255},
		Selection:      color.NRGBA{R: 180, G: 230, B: 210, A: 100},
		Hover:          color.NRGBA{R: 210, G: 245, B: 235, A: 100},
		ScrollBar:      color.NRGBA{R: 160, G: 195, B: 185, A: 255},
		Placeholder:    color.NRGBA{R: 120, G: 150, B: 140, A: 255},
	},
	{
		Name:           "Rose Petal",
		Mode:           "light",
		Primary:        color.NRGBA{R: 200, G: 80, B: 120, A: 255},
		Background:     color.NRGBA{R: 255, G: 250, B: 252, A: 255},
		Foreground:     color.NRGBA{R: 50, G: 35, B: 40, A: 255},
		Button:         color.NRGBA{R: 248, G: 235, B: 240, A: 255},
		ButtonDisabled: color.NRGBA{R: 235, G: 225, B: 228, A: 255},
		Input:          color.NRGBA{R: 255, G: 252, B: 254, A: 255},
		Menu:           color.NRGBA{R: 252, G: 242, B: 246, A: 255},
		Separator:      color.NRGBA{R: 230, G: 200, B: 210, A: 255},
		Selection:      color.NRGBA{R: 255, G: 200, B: 220, A: 100},
		Hover:          color.NRGBA{R: 255, G: 230, B: 240, A: 100},
		ScrollBar:      color.NRGBA{R: 210, G: 175, B: 190, A: 255},
		Placeholder:    color.NRGBA{R: 170, G: 140, B: 150, A: 255},
	},
	{
		Name:           "Sky Blue",
		Mode:           "light",
		Primary:        color.NRGBA{R: 30, G: 144, B: 200, A: 255},
		Background:     color.NRGBA{R: 245, G: 250, B: 255, A: 255},
		Foreground:     color.NRGBA{R: 25, G: 35, B: 45, A: 255},
		Button:         color.NRGBA{R: 225, G: 238, B: 250, A: 255},
		ButtonDisabled: color.NRGBA{R: 210, G: 225, B: 240, A: 255},
		Input:          color.NRGBA{R: 252, G: 254, B: 255, A: 255},
		Menu:           color.NRGBA{R: 238, G: 246, B: 252, A: 255},
		Separator:      color.NRGBA{R: 190, G: 210, B: 230, A: 255},
		Selection:      color.NRGBA{R: 180, G: 220, B: 255, A: 100},
		Hover:          color.NRGBA{R: 210, G: 235, B: 255, A: 100},
		ScrollBar:      color.NRGBA{R: 160, G: 190, B: 215, A: 255},
		Placeholder:    color.NRGBA{R: 120, G: 145, B: 170, A: 255},
	},
	{
		Name:           "Lavender Mist",
		Mode:           "light",
		Primary:        color.NRGBA{R: 140, G: 100, B: 180, A: 255},
		Background:     color.NRGBA{R: 252, G: 250, B: 255, A: 255},
		Foreground:     color.NRGBA{R: 40, G: 35, B: 50, A: 255},
		Button:         color.NRGBA{R: 240, G: 235, B: 248, A: 255},
		ButtonDisabled: color.NRGBA{R: 228, G: 224, B: 238, A: 255},
		Input:          color.NRGBA{R: 254, G: 252, B: 255, A: 255},
		Menu:           color.NRGBA{R: 248, G: 244, B: 252, A: 255},
		Separator:      color.NRGBA{R: 215, G: 205, B: 230, A: 255},
		Selection:      color.NRGBA{R: 220, G: 200, B: 250, A: 100},
		Hover:          color.NRGBA{R: 240, G: 230, B: 255, A: 100},
		ScrollBar:      color.NRGBA{R: 190, G: 175, B: 210, A: 255},
		Placeholder:    color.NRGBA{R: 150, G: 140, B: 170, A: 255},
	},
	{
		Name:           "Sunny Day",
		Mode:           "light",
		Primary:        color.NRGBA{R: 220, G: 160, B: 30, A: 255},
		Background:     color.NRGBA{R: 255, G: 254, B: 248, A: 255},
		Foreground:     color.NRGBA{R: 50, G: 45, B: 30, A: 255},
		Button:         color.NRGBA{R: 250, G: 245, B: 230, A: 255},
		ButtonDisabled: color.NRGBA{R: 240, G: 235, B: 220, A: 255},
		Input:          color.NRGBA{R: 255, G: 255, B: 252, A: 255},
		Menu:           color.NRGBA{R: 252, G: 250, B: 240, A: 255},
		Separator:      color.NRGBA{R: 230, G: 220, B: 190, A: 255},
		Selection:      color.NRGBA{R: 255, G: 240, B: 180, A: 100},
		Hover:          color.NRGBA{R: 255, G: 250, B: 220, A: 100},
		ScrollBar:      color.NRGBA{R: 210, G: 195, B: 150, A: 255},
		Placeholder:    color.NRGBA{R: 170, G: 160, B: 130, A: 255},
	},
	{
		Name:           "Clean White",
		Mode:           "light",
		Primary:        color.NRGBA{R: 80, G: 80, B: 90, A: 255},
		Background:     color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Foreground:     color.NRGBA{R: 30, G: 30, B: 30, A: 255},
		Button:         color.NRGBA{R: 242, G: 242, B: 244, A: 255},
		ButtonDisabled: color.NRGBA{R: 230, G: 230, B: 232, A: 255},
		Input:          color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Menu:           color.NRGBA{R: 250, G: 250, B: 252, A: 255},
		Separator:      color.NRGBA{R: 220, G: 220, B: 225, A: 255},
		Selection:      color.NRGBA{R: 210, G: 215, B: 225, A: 100},
		Hover:          color.NRGBA{R: 235, G: 238, B: 242, A: 100},
		ScrollBar:      color.NRGBA{R: 195, G: 195, B: 200, A: 255},
		Placeholder:    color.NRGBA{R: 150, G: 150, B: 158, A: 255},
	},
	{
		Name:           "Coral Reef",
		Mode:           "light",
		Primary:        color.NRGBA{R: 255, G: 127, B: 80, A: 255},
		Background:     color.NRGBA{R: 255, G: 252, B: 250, A: 255},
		Foreground:     color.NRGBA{R: 55, G: 40, B: 35, A: 255},
		Button:         color.NRGBA{R: 252, G: 242, B: 238, A: 255},
		ButtonDisabled: color.NRGBA{R: 242, G: 232, B: 228, A: 255},
		Input:          color.NRGBA{R: 255, G: 254, B: 252, A: 255},
		Menu:           color.NRGBA{R: 254, G: 248, B: 245, A: 255},
		Separator:      color.NRGBA{R: 235, G: 215, B: 205, A: 255},
		Selection:      color.NRGBA{R: 255, G: 200, B: 170, A: 100},
		Hover:          color.NRGBA{R: 255, G: 230, B: 215, A: 100},
		ScrollBar:      color.NRGBA{R: 220, G: 185, B: 165, A: 255},
		Placeholder:    color.NRGBA{R: 180, G: 150, B: 135, A: 255},
	},
}

// ColorBlindThemes contains color safe color schemes (9 themes)
var ColorBlindThemes = []ThemeScheme{
	{
		Name:           "Blue Orange",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 230, G: 159, B: 0, A: 255},
		Background:     color.NRGBA{R: 20, G: 30, B: 45, A: 255},
		Foreground:     color.NRGBA{R: 240, G: 240, B: 245, A: 255},
		Button:         color.NRGBA{R: 35, G: 50, B: 70, A: 255},
		ButtonDisabled: color.NRGBA{R: 28, G: 40, B: 55, A: 255},
		Input:          color.NRGBA{R: 30, G: 45, B: 65, A: 255},
		Menu:           color.NRGBA{R: 25, G: 38, B: 55, A: 255},
		Separator:      color.NRGBA{R: 55, G: 75, B: 100, A: 255},
		Selection:      color.NRGBA{R: 130, G: 100, B: 40, A: 100},
		Hover:          color.NRGBA{R: 100, G: 80, B: 30, A: 100},
		ScrollBar:      color.NRGBA{R: 140, G: 110, B: 50, A: 255},
		Placeholder:    color.NRGBA{R: 150, G: 160, B: 175, A: 255},
	},
	{
		Name:           "Blue Yellow",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 240, G: 228, B: 66, A: 255},
		Background:     color.NRGBA{R: 15, G: 32, B: 65, A: 255},
		Foreground:     color.NRGBA{R: 245, G: 245, B: 250, A: 255},
		Button:         color.NRGBA{R: 25, G: 50, B: 90, A: 255},
		ButtonDisabled: color.NRGBA{R: 20, G: 40, B: 72, A: 255},
		Input:          color.NRGBA{R: 22, G: 45, B: 82, A: 255},
		Menu:           color.NRGBA{R: 18, G: 38, B: 72, A: 255},
		Separator:      color.NRGBA{R: 40, G: 70, B: 115, A: 255},
		Selection:      color.NRGBA{R: 120, G: 115, B: 50, A: 100},
		Hover:          color.NRGBA{R: 95, G: 92, B: 40, A: 100},
		ScrollBar:      color.NRGBA{R: 130, G: 125, B: 60, A: 255},
		Placeholder:    color.NRGBA{R: 140, G: 155, B: 185, A: 255},
	},
	{
		Name:           "High Contrast",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Background:     color.NRGBA{R: 0, G: 0, B: 0, A: 255},
		Foreground:     color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Button:         color.NRGBA{R: 40, G: 40, B: 40, A: 255},
		ButtonDisabled: color.NRGBA{R: 25, G: 25, B: 25, A: 255},
		Input:          color.NRGBA{R: 30, G: 30, B: 30, A: 255},
		Menu:           color.NRGBA{R: 20, G: 20, B: 20, A: 255},
		Separator:      color.NRGBA{R: 100, G: 100, B: 100, A: 255},
		Selection:      color.NRGBA{R: 80, G: 80, B: 80, A: 150},
		Hover:          color.NRGBA{R: 60, G: 60, B: 60, A: 150},
		ScrollBar:      color.NRGBA{R: 120, G: 120, B: 120, A: 255},
		Placeholder:    color.NRGBA{R: 180, G: 180, B: 180, A: 255},
	},
	{
		Name:           "Monochrome Blue",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 100, G: 180, B: 255, A: 255},
		Background:     color.NRGBA{R: 10, G: 20, B: 35, A: 255},
		Foreground:     color.NRGBA{R: 220, G: 235, B: 255, A: 255},
		Button:         color.NRGBA{R: 20, G: 40, B: 65, A: 255},
		ButtonDisabled: color.NRGBA{R: 15, G: 30, B: 50, A: 255},
		Input:          color.NRGBA{R: 18, G: 35, B: 58, A: 255},
		Menu:           color.NRGBA{R: 14, G: 28, B: 48, A: 255},
		Separator:      color.NRGBA{R: 35, G: 65, B: 100, A: 255},
		Selection:      color.NRGBA{R: 50, G: 100, B: 150, A: 100},
		Hover:          color.NRGBA{R: 40, G: 80, B: 120, A: 100},
		ScrollBar:      color.NRGBA{R: 60, G: 110, B: 160, A: 255},
		Placeholder:    color.NRGBA{R: 120, G: 150, B: 190, A: 255},
	},
	{
		Name:           "Warm Safe",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 204, G: 121, B: 167, A: 255},
		Background:     color.NRGBA{R: 35, G: 30, B: 32, A: 255},
		Foreground:     color.NRGBA{R: 245, G: 240, B: 242, A: 255},
		Button:         color.NRGBA{R: 55, G: 48, B: 52, A: 255},
		ButtonDisabled: color.NRGBA{R: 45, G: 40, B: 42, A: 255},
		Input:          color.NRGBA{R: 50, G: 44, B: 47, A: 255},
		Menu:           color.NRGBA{R: 42, G: 36, B: 39, A: 255},
		Separator:      color.NRGBA{R: 80, G: 70, B: 75, A: 255},
		Selection:      color.NRGBA{R: 120, G: 80, B: 100, A: 100},
		Hover:          color.NRGBA{R: 100, G: 65, B: 82, A: 100},
		ScrollBar:      color.NRGBA{R: 130, G: 90, B: 110, A: 255},
		Placeholder:    color.NRGBA{R: 160, G: 150, B: 155, A: 255},
	},
	{
		Name:           "Deuteranopia",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 86, G: 180, B: 233, A: 255},
		Background:     color.NRGBA{R: 25, G: 25, B: 30, A: 255},
		Foreground:     color.NRGBA{R: 240, G: 240, B: 245, A: 255},
		Button:         color.NRGBA{R: 45, G: 45, B: 55, A: 255},
		ButtonDisabled: color.NRGBA{R: 35, G: 35, B: 42, A: 255},
		Input:          color.NRGBA{R: 40, G: 40, B: 50, A: 255},
		Menu:           color.NRGBA{R: 32, G: 32, B: 40, A: 255},
		Separator:      color.NRGBA{R: 65, G: 70, B: 85, A: 255},
		Selection:      color.NRGBA{R: 60, G: 110, B: 145, A: 100},
		Hover:          color.NRGBA{R: 50, G: 90, B: 120, A: 100},
		ScrollBar:      color.NRGBA{R: 70, G: 120, B: 160, A: 255},
		Placeholder:    color.NRGBA{R: 145, G: 155, B: 170, A: 255},
	},
	{
		Name:           "Protanopia",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 0, G: 158, B: 115, A: 255},
		Background:     color.NRGBA{R: 20, G: 28, B: 25, A: 255},
		Foreground:     color.NRGBA{R: 235, G: 245, B: 240, A: 255},
		Button:         color.NRGBA{R: 35, G: 50, B: 45, A: 255},
		ButtonDisabled: color.NRGBA{R: 28, G: 40, B: 36, A: 255},
		Input:          color.NRGBA{R: 32, G: 45, B: 40, A: 255},
		Menu:           color.NRGBA{R: 26, G: 38, B: 34, A: 255},
		Separator:      color.NRGBA{R: 50, G: 75, B: 65, A: 255},
		Selection:      color.NRGBA{R: 40, G: 100, B: 80, A: 100},
		Hover:          color.NRGBA{R: 30, G: 80, B: 65, A: 100},
		ScrollBar:      color.NRGBA{R: 50, G: 110, B: 90, A: 255},
		Placeholder:    color.NRGBA{R: 130, G: 160, B: 150, A: 255},
	},
	{
		Name:           "Tritanopia",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 213, G: 94, B: 0, A: 255},
		Background:     color.NRGBA{R: 30, G: 25, B: 22, A: 255},
		Foreground:     color.NRGBA{R: 245, G: 240, B: 235, A: 255},
		Button:         color.NRGBA{R: 55, G: 45, B: 40, A: 255},
		ButtonDisabled: color.NRGBA{R: 42, G: 36, B: 32, A: 255},
		Input:          color.NRGBA{R: 48, G: 40, B: 36, A: 255},
		Menu:           color.NRGBA{R: 38, G: 32, B: 28, A: 255},
		Separator:      color.NRGBA{R: 80, G: 65, B: 55, A: 255},
		Selection:      color.NRGBA{R: 130, G: 70, B: 40, A: 100},
		Hover:          color.NRGBA{R: 105, G: 55, B: 30, A: 100},
		ScrollBar:      color.NRGBA{R: 140, G: 80, B: 50, A: 255},
		Placeholder:    color.NRGBA{R: 170, G: 155, B: 145, A: 255},
	},
	{
		Name:           "Grayscale",
		Mode:           "colorblind",
		Primary:        color.NRGBA{R: 200, G: 200, B: 200, A: 255},
		Background:     color.NRGBA{R: 25, G: 25, B: 25, A: 255},
		Foreground:     color.NRGBA{R: 240, G: 240, B: 240, A: 255},
		Button:         color.NRGBA{R: 50, G: 50, B: 50, A: 255},
		ButtonDisabled: color.NRGBA{R: 38, G: 38, B: 38, A: 255},
		Input:          color.NRGBA{R: 42, G: 42, B: 42, A: 255},
		Menu:           color.NRGBA{R: 35, G: 35, B: 35, A: 255},
		Separator:      color.NRGBA{R: 75, G: 75, B: 75, A: 255},
		Selection:      color.NRGBA{R: 100, G: 100, B: 100, A: 100},
		Hover:          color.NRGBA{R: 80, G: 80, B: 80, A: 100},
		ScrollBar:      color.NRGBA{R: 110, G: 110, B: 110, A: 255},
		Placeholder:    color.NRGBA{R: 150, G: 150, B: 150, A: 255},
	},
}

// CassBrowserTheme is a custom theme for the Cassandra Browser
type CassBrowserTheme struct {
	scheme         *ThemeScheme
	customFontSize float32 // User-selected font size (0 means use default)
}

// NewCassBrowserThemeWithScheme creates a theme with a specific scheme
func NewCassBrowserThemeWithScheme(schemeName string) *CassBrowserTheme {
	scheme := FindThemeScheme(schemeName)
	if scheme == nil {
		scheme = &DarkThemes[0]
	}
	return &CassBrowserTheme{
		scheme: scheme,
	}
}

// GetScheme returns the current scheme
func (t *CassBrowserTheme) GetScheme() *ThemeScheme {
	return t.scheme
}

// FindThemeScheme finds a theme scheme by name
func FindThemeScheme(name string) *ThemeScheme {
	for i := range DarkThemes {
		if DarkThemes[i].Name == name {
			return &DarkThemes[i]
		}
	}
	for i := range LightThemes {
		if LightThemes[i].Name == name {
			return &LightThemes[i]
		}
	}
	for i := range ColorBlindThemes {
		if ColorBlindThemes[i].Name == name {
			return &ColorBlindThemes[i]
		}
	}
	return nil
}

// Color returns the color for the specified theme color name
func (t *CassBrowserTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if t.scheme == nil {
		t.scheme = &DarkThemes[0]
	}

	switch name {
	case theme.ColorNamePrimary:
		return t.scheme.Primary
	case theme.ColorNameFocus:
		return t.scheme.Primary
	case theme.ColorNameSelection:
		return t.scheme.Selection
	case theme.ColorNameHover:
		return t.scheme.Hover
	case theme.ColorNameBackground:
		return t.scheme.Background
	case theme.ColorNameButton:
		return t.scheme.Button
	case theme.ColorNameDisabledButton:
		return t.scheme.ButtonDisabled
	case theme.ColorNameDisabled:
		return t.scheme.Placeholder
	case theme.ColorNamePlaceHolder:
		return t.scheme.Placeholder
	case theme.ColorNameScrollBar:
		return t.scheme.ScrollBar
	case theme.ColorNameShadow:
		if t.scheme.Mode == "light" {
			return color.NRGBA{R: 0, G: 0, B: 0, A: 120}
		}
		return color.NRGBA{R: 0, G: 0, B: 0, A: 200}
	case theme.ColorNameInputBackground:
		return t.scheme.Input
	case theme.ColorNameMenuBackground:
		// Make menu slightly lighter than the defined menu color for better visibility
		r, g, b, _ := t.scheme.Menu.RGBA()
		rr, gg, bb := uint8(r>>8), uint8(g>>8), uint8(b>>8)
		if t.scheme.Mode == "light" {
			// For light mode, darken slightly
			rr = uint8(max(0, int(rr)-10))
			gg = uint8(max(0, int(gg)-10))
			bb = uint8(max(0, int(bb)-10))
		} else {
			// For dark mode, lighten slightly
			rr = uint8(min(255, int(rr)+15))
			gg = uint8(min(255, int(gg)+15))
			bb = uint8(min(255, int(bb)+15))
		}
		return color.NRGBA{R: rr, G: gg, B: bb, A: 255}
	case theme.ColorNameOverlayBackground:
		r, g, b, _ := t.scheme.Menu.RGBA()
		return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 250}
	case theme.ColorNameSeparator:
		return t.scheme.Separator
	case theme.ColorNameForeground:
		return t.scheme.Foreground
	case theme.ColorNameError:
		return color.NRGBA{R: 200, G: 50, B: 50, A: 255}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 50, G: 200, B: 50, A: 255}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 200, G: 200, B: 50, A: 255}
	default:
		return theme.DefaultTheme().Color(name, variant)
	}
}

// Font returns the font for the specified text style
func (t *CassBrowserTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

// SetCustomFontSize sets the custom font size for the theme
func (t *CassBrowserTheme) SetCustomFontSize(size float32) {
	t.customFontSize = size
}

// Icon returns the icon for the specified icon name
func (t *CassBrowserTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// Size returns the size for the specified size name
func (t *CassBrowserTheme) Size(name fyne.ThemeSizeName) float32 {
	// Base text size - use custom if set, otherwise default to 14
	baseSize := float32(14)
	if t.customFontSize > 0 {
		baseSize = t.customFontSize
	}

	switch name {
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInlineIcon:
		// Scale icons proportionally with font size
		return baseSize * 1.4
	case theme.SizeNameScrollBar:
		return 12
	case theme.SizeNameScrollBarSmall:
		return 6
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameText:
		return baseSize
	case theme.SizeNameHeadingText:
		// Heading is ~1.4x base size
		return baseSize * 1.4
	case theme.SizeNameSubHeadingText:
		// Subheading is ~1.15x base size
		return baseSize * 1.15
	case theme.SizeNameCaptionText:
		// Caption is ~0.85x base size
		return baseSize * 0.85
	case theme.SizeNameInputBorder:
		return 2
	default:
		return theme.DefaultTheme().Size(name)
	}
}

// ansi256ToColor converts an ANSI 256 color code to a Go color
func ansi256ToColor(code string) color.Color {
	n, err := strconv.Atoi(code)
	if err != nil {
		return color.NRGBA{R: 221, G: 176, B: 56, A: 255} // Default gold
	}

	// Standard colors (0-15)
	if n < 16 {
		standardColors := []color.NRGBA{
			{R: 0, G: 0, B: 0, A: 255},       // 0: Black
			{R: 128, G: 0, B: 0, A: 255},     // 1: Red
			{R: 0, G: 128, B: 0, A: 255},     // 2: Green
			{R: 128, G: 128, B: 0, A: 255},   // 3: Yellow
			{R: 0, G: 0, B: 128, A: 255},     // 4: Blue
			{R: 128, G: 0, B: 128, A: 255},   // 5: Magenta
			{R: 0, G: 128, B: 128, A: 255},   // 6: Cyan
			{R: 192, G: 192, B: 192, A: 255}, // 7: White
			{R: 128, G: 128, B: 128, A: 255}, // 8: Bright Black
			{R: 255, G: 0, B: 0, A: 255},     // 9: Bright Red
			{R: 0, G: 255, B: 0, A: 255},     // 10: Bright Green
			{R: 255, G: 255, B: 0, A: 255},   // 11: Bright Yellow
			{R: 0, G: 0, B: 255, A: 255},     // 12: Bright Blue
			{R: 255, G: 0, B: 255, A: 255},   // 13: Bright Magenta
			{R: 0, G: 255, B: 255, A: 255},   // 14: Bright Cyan
			{R: 255, G: 255, B: 255, A: 255}, // 15: Bright White
		}
		return standardColors[n]
	}

	// 216 color cube (16-231)
	if n < 232 {
		n -= 16
		r := uint8((n / 36) * 51)
		g := uint8(((n / 6) % 6) * 51)
		b := uint8((n % 6) * 51)
		return color.NRGBA{R: r, G: g, B: b, A: 255}
	}

	// Grayscale (232-255)
	gray := uint8((n-232)*10 + 8)
	return color.NRGBA{R: gray, G: gray, B: gray, A: 255}
}

func newErrorText(msg string, size float32) *canvas.Text {
	t := canvas.NewText(msg, theme.ErrorColor())
	if size == 0 {
		t.TextSize = theme.TextSize()
	} else {
		t.TextSize = size
	}
	return t
}

func newSuccessText(msg string, size float32) *canvas.Text {
	t := canvas.NewText(msg, color.RGBA{R: 40, G: 167, B: 69, A: 255})
	if size == 0 {
		t.TextSize = theme.TextSize()
	} else {
		t.TextSize = size
	}
	return t
}
