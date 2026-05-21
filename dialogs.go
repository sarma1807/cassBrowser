package main

import (
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ShowDeleteDialog displays the delete confirmation dialog
func ShowDeleteDialog(w fyne.Window, conn *DecryptedConnection, onDelete func()) {
	content := container.NewVBox(
		widget.NewLabel("Are you sure you want to delete this connection?"),
		widget.NewLabel(""),
		widget.NewLabel("Name: "+conn.ConnName),
		widget.NewLabel("Environment: "+conn.Environment),
		widget.NewLabel(""),
		widget.NewLabelWithStyle("This action cannot be undone!", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	)

	d := dialog.NewCustomConfirm("Delete Connection", "Delete", "Cancel", content, func(delete bool) {
		if delete && onDelete != nil {
			onDelete()
		}
	}, w)

	d.SetDismissText("Cancel")
	d.Show()
}

// ShowRenameDialog displays the rename dialog
func ShowRenameDialog(w fyne.Window, conn *DecryptedConnection, onRename func(string)) {
	entry := widget.NewEntry()
	entry.SetText(conn.ConnName)
	entry.SetPlaceHolder("New connection name")

	errorLabel := widget.NewLabel("")

	content := container.NewVBox(
		widget.NewLabel("Enter new name for connection :"),
		widget.NewLabel("Current: "+conn.ConnName),
		widget.NewSeparator(),
		entry,
		errorLabel,
	)

	d := dialog.NewCustomConfirm("Rename Connection", "Rename", "Cancel", content, func(rename bool) {
		if rename {
			newName := entry.Text
			if newName == "" {
				return
			}
			if len(newName) < 5 {
				dialog.ShowError(errors.New("name must be at least 5 characters"), w)
				return
			}
			if onRename != nil {
				onRename(newName)
			}
		}
	}, w)

	d.Resize(fyne.NewSize(400, 200))
	d.Show()
}
