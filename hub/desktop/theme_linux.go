//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>

static void vtc_prefer_dark_theme(void) {
	if (!gtk_init_check(NULL, NULL)) return;
	GtkSettings *settings = gtk_settings_get_default();
	if (settings != NULL) {
		g_object_set(G_OBJECT(settings), "gtk-application-prefer-dark-theme", TRUE, NULL);
	}
}
*/
import "C"

import (
	"os"
	"runtime"
)

// GTK must be driven from the thread that initialised it, which is the one
// the window toolkit runs on: the main goroutine, held on the main thread
// from the start.
func init() {
	runtime.LockOSThread()
}

// preferDarkTheme asks GTK for the dark variant of the user's theme, so the
// menu bar and dialogs match the page instead of sitting light above it. The
// theme itself stays the user's; a theme without a dark variant is unchanged.
//
// GTK is initialised here, before the toolkit does it, so the toolkit's choice
// of display backend is repeated first: X11 unless the session says otherwise.
func preferDarkTheme() {
	session := os.Getenv("XDG_SESSION_TYPE")
	if os.Getenv("GDK_BACKEND") == "" && (session == "" || session == "unspecified" || session == "x11") {
		_ = os.Setenv("GDK_BACKEND", "x11") //nolint:errcheck // GTK picks a backend itself without it
	}
	C.vtc_prefer_dark_theme()
}
