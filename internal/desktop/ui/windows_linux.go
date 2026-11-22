//go:build desktop && linux

package ui

import (
	"context"

	"github.com/ncruces/zenity"
)

func (a *App) handlePresentationAction(action string) bool {
	if action == "settings" || action == "details" {
		if err := a.presentation.dashboard.openWindow(action); err != nil {
			a.action(func(context.Context) error { return err })
		}
		return true
	}
	return a.handleWindowAction(action)
}
func (a *App) nativeQuestion(string, string) (bool, error) { return false, nil }
func (a *App) showMessage(title, message string, failure bool) error {
	if failure {
		return zenity.Error(message, a.options(title)...)
	}
	return zenity.Info(message, a.options(title)...)
}
