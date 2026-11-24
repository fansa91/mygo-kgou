// Package localfolder reads a folder of music on a phone where it is: the
// system hands the app a folder, and the app keeps the system's permission
// to it for the next run, so the songs play from the folder itself instead of
// being copied into the app.
//
// The folder comes from the plugin's native code, listed beside this package
// in mygo-plugin.json: Swift on iOS, which keeps the permission in a
// bookmark, and Kotlin on Android, which keeps the path and reads it with the
// audio permission. A desktop has no such code — a folder there is picked with
// the system's dialog, and read by path as it always was — and Available
// reports false.
package localfolder

import (
	"context"

	"github.com/egoist/mygo"
)

// plugin is the id the native code is registered under, as mygo-plugin.json
// says.
const plugin = "localfolder"

// Available reports whether the app has the plugin's native code: on iOS and
// Android, in a build that imports this package.
func Available() bool { return mygo.Native(plugin).Available() }

// Restored is the folder picked in an earlier run, as this run finds it.
type Restored struct {
	// Path is the folder to read, or "" when there is none to go back to.
	Path string
	// Saved says whether a folder was picked before: with no Path, the system
	// no longer lets the app read it, and the user has to pick it again.
	Saved bool
}

// answer is what the native code replies with.
type answer struct {
	Path  string `json:"path"`
	Saved bool   `json:"saved"`
}

// Pick asks the user for a folder, and returns its path, or "" when they
// chose none. What they chose is kept: Restore takes it back in the next run.
func Pick(ctx context.Context) (string, error) {
	var a answer
	if err := mygo.Native(plugin).Call(ctx, "pick", nil, &a); err != nil {
		return "", err
	}
	return a.Path, nil
}

// Restore takes back the permission to the folder picked before, and says
// whether it is still there to read.
func Restore(ctx context.Context) (Restored, error) {
	var a answer
	if err := mygo.Native(plugin).Call(ctx, "restore", nil, &a); err != nil {
		return Restored{}, err
	}
	return Restored{Path: a.Path, Saved: a.Saved}, nil
}

// Forget drops the folder the app kept, and the system's permission to it.
func Forget(ctx context.Context) error {
	return mygo.Native(plugin).Call(ctx, "forget", nil, nil)
}
