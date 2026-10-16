//go:build !linux

package main

// preferDarkTheme is Linux's concern; macOS gets the dark appearance from the
// window options.
func preferDarkTheme() {}
