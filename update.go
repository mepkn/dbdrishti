package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// commit is the short SHA this build was made from, set by the release build
// (-ldflags "-X main.commit=..."). Releases are tagged with the same short SHA,
// so a different tag on the latest release means an update is available.
var commit = "dev"

const (
	updateName   = "DBDrishti"
	updateRepo   = "mepkn/dbdrishti"
	updateAsset  = "dbdrishti-macos.zip"
	updatePrefix = "dbdrishti-update-"
)

type updater struct {
	app  *application.App
	busy atomic.Bool
}

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// cleanup removes what an earlier update left in the temp folder.
func (u *updater) cleanup() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), updatePrefix+"*"))
	for _, d := range dirs {
		os.RemoveAll(d)
	}
}

// check looks for a newer release. manual is true when started from the menu:
// then "up to date" and errors are shown too, not only a found update.
func (u *updater) check(manual bool) {
	if !u.busy.CompareAndSwap(false, true) {
		return
	}
	defer u.busy.Store(false)

	if commit == "dev" {
		if manual {
			u.info("Updates are off", "This is a development build.")
		}
		return
	}
	rel, err := latestRelease()
	if err != nil {
		if manual {
			u.fail(err)
		}
		return
	}
	if rel.Tag == commit {
		if manual {
			u.info("You're up to date", fmt.Sprintf("%s %s is the latest version.", updateName, commit))
		}
		return
	}
	var url string
	for _, a := range rel.Assets {
		if a.Name == updateAsset {
			url = a.URL
		}
	}
	if url == "" {
		if manual {
			u.fail(fmt.Errorf("release %s has no %s", rel.Tag, updateAsset))
		}
		return
	}

	d := u.app.Dialog.Question().
		SetTitle("Update available").
		SetMessage(fmt.Sprintf("%s %s is available (you have %s). Update and restart now?", updateName, rel.Tag, commit))
	ok := d.AddButton("Update & Restart")
	ok.SetAsDefault().OnClick(func() {
		go func() {
			if err := u.install(url); err != nil {
				u.fail(err)
			}
		}()
	})
	d.AddButton("Later").SetAsCancel()
	d.Show()
}

func latestRelease() (*release, error) {
	client := http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", res.Status)
	}
	var rel release
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// install downloads and unpacks the new app, then hands over to a script that
// waits for the app to quit, swaps the bundle, deletes the download and relaunches.
func (u *updater) install(url string) error {
	bundle, err := appBundle()
	if err != nil {
		return err
	}
	// The swap renames inside the bundle's folder, so it must be writable.
	probe, err := os.CreateTemp(filepath.Dir(bundle), "."+updatePrefix+"write-test")
	if err != nil {
		return fmt.Errorf("can't replace %s (folder not writable): %w", bundle, err)
	}
	probe.Close()
	os.Remove(probe.Name())

	tmp, err := os.MkdirTemp("", updatePrefix)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		os.RemoveAll(tmp)
		return err
	}

	zip := filepath.Join(tmp, updateAsset)
	if err := download(url, zip); err != nil {
		return fail(err)
	}
	out := filepath.Join(tmp, "app")
	if err := exec.Command("ditto", "-x", "-k", zip, out).Run(); err != nil {
		return fail(fmt.Errorf("unzip: %w", err))
	}
	apps, _ := filepath.Glob(filepath.Join(out, "*.app"))
	if len(apps) != 1 {
		return fail(errors.New("the download has no app in it"))
	}
	newApp := apps[0]
	if _, err := os.Stat(filepath.Join(newApp, "Contents", "MacOS")); err != nil {
		return fail(errors.New("the downloaded app is incomplete"))
	}
	exec.Command("xattr", "-dr", "com.apple.quarantine", newApp).Run()

	script := filepath.Join(tmp, "swap.sh")
	if err := os.WriteFile(script, []byte(swapScript), 0o700); err != nil {
		return fail(err)
	}
	cmd := exec.Command("/bin/sh", script, fmt.Sprint(os.Getpid()), bundle, newApp, tmp)
	// Own session, so the script outlives the app.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	u.app.Quit()
	return nil
}

// swapScript: $1 the app's pid, $2 installed bundle, $3 new bundle, $4 temp dir.
// If the new bundle can't be moved in, the old one is put back.
const swapScript = `#!/bin/sh
pid=$1 old=$2 new=$3 tmp=$4
while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
rm -rf "$old.old"
if mv "$old" "$old.old"; then
	if mv "$new" "$old"; then
		rm -rf "$old.old"
	else
		mv "$old.old" "$old"
	fi
fi
rm -rf "$tmp"
open "$old"
`

// appBundle is the .app this runs from (…/X.app/Contents/MacOS/x).
func appBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") {
		return "", errors.New("not running from an .app bundle")
	}
	return bundle, nil
}

func download(url, path string) error {
	client := http.Client{Timeout: 10 * time.Minute}
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", res.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (u *updater) info(title, msg string) {
	u.app.Dialog.Info().SetTitle(title).SetMessage(msg).Show()
}

func (u *updater) fail(err error) {
	u.app.Dialog.Error().SetTitle("Update failed").SetMessage(err.Error()).Show()
}
