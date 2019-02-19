package updater

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Updater provides a configuration on how to update
type Updater struct {
	upstream string
	interval time.Duration
	execPath string
}

// NewUpdater creates Updater with upstream to be pointing to a url to get the
// newest binary file. interval sets how frequent the polling
func NewUpdater(upstream string, interval time.Duration) (*Updater, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	updater := Updater{
		upstream: upstream,
		interval: interval,
		execPath: execPath,
	}

	return &updater, nil
}

// RestartAndUpdate swaps the old process with a new process
func (u *Updater) RestartAndUpdate() error {
	var err error
	if err = os.Rename(u.execPath, u.execPath+".old"); err != nil {
		return err
	}

	_, err = os.StartProcess(u.execPath, os.Args, &os.ProcAttr{
		Dir:   filepath.Dir(u.execPath),
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys:   &syscall.SysProcAttr{},
	})
	if err != nil {
		return err
	}

	old, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}

	return old.Kill()
}

// StartUpdater will start updating within intervals and keep sending
// the newest info
func (u *Updater) StartUpdater(ctx context.Context) <-chan Info {
	ticker := time.NewTicker(u.interval)
	infoChan := make(chan Info)

	go func() {
	loop:
		for {
			select {
			case <-ticker.C:
				info, err := u.update()
				if err != nil {
					continue
				}
				infoChan <- info
			case <-ctx.Done():
				break loop
			}
		}
	}()
	return infoChan
}

// update fetches and unpack to <execPath>.new
func (u *Updater) update() (Info, error) {
	archivePath, err := u.fetch()
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(archivePath)
	return u.unpack(archivePath)
}

// fetch downloads the newest archive and returns the path to where it's saved
func (u *Updater) fetch() (string, error) {
	resp, err := http.Get(u.upstream)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmp, err := ioutil.TempFile("", "")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	// Download an archive
	_, err = io.Copy(tmp, resp.Body)
	return tmp.Name(), err
}

// unpack extracts the archive located at <path> and move the binary file to
// <execPath>.new, and returns the metadata about the binary file
func (u *Updater) unpack(path string) (Info, error) {
	var info Info
	var binaryFile, infoFile io.ReadCloser

	r, err := zip.OpenReader(path)
	if err != nil {
		return info, err
	}
	defer r.Close()

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return info, err
		}

		// TODO! probably a better way than hardcoded
		switch f.Name {
		case "info.json":
			infoFile = rc
		case u.execPath:
			binaryFile = rc
		}
	}

	if infoFile == nil || binaryFile == nil {
		return info, errors.New("info.json or binary file doesn't exist in archive")
	}

	defer infoFile.Close()
	defer binaryFile.Close()

	// Parse info.json
	if err = json.NewDecoder(infoFile).Decode(&info); err != nil {
		return info, err
	}

	newFile, err := os.Create(u.execPath + ".new")
	if err != nil {
		return info, err
	}
	defer newFile.Close()

	_, err = io.Copy(newFile, binaryFile)
	return info, err
}
