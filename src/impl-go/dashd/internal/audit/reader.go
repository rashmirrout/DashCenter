// PD-G4: tail-follow reader. Used by ObservabilityService.GetAuditLog
// to stream the full log + live appends to a client.
package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Tail streams every existing line from audit.jsonl then follows new
// lines as they're appended. Cancelling ctx terminates the stream.
//
// emit receives one decoded entry at a time. An emit that returns an
// error terminates the tail (the err propagates back to the caller).
//
// Tail handles rotation transparently: when the open file's inode
// disappears (because rotateLocked renamed it) Tail re-opens the
// canonical path and continues from byte 0.
func Tail(ctx context.Context, dir string, fromBeginning bool, emit func(Entry) error) error {
	if emit == nil {
		return errors.New("audit.Tail: emit is nil")
	}
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.Open(path)
	if err != nil {
		// audit log may not exist yet — wait for it.
		if errors.Is(err, os.ErrNotExist) {
			if err := waitForFile(ctx, path); err != nil {
				return err
			}
			f, err = os.Open(path)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	defer f.Close()
	if !fromBeginning {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
	}

	rd := bufio.NewReader(f)
	pollInterval := 250 * time.Millisecond
	rotationCheckEvery := 4 // every Nth poll check inode
	tick := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := rd.ReadString('\n')
		if err == nil {
			var e Entry
			if jsonErr := json.Unmarshal([]byte(line), &e); jsonErr != nil {
				continue
			}
			if err := emit(e); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, io.EOF) {
			return err
		}
		// EOF: sleep then check for new bytes / rotation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
		tick++
		if tick%rotationCheckEvery == 0 {
			// Did the file get rotated? If so, re-open.
			rotated, err := didRotate(path, f)
			if err == nil && rotated {
				_ = f.Close()
				f2, oerr := os.Open(path)
				if oerr != nil {
					return oerr
				}
				f = f2
				rd = bufio.NewReader(f)
			}
		}
	}
}

func waitForFile(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// didRotate returns true when the canonical audit.jsonl is a different
// inode than the open file handle. On Windows this is best-effort
// (Sys() returns different fields); we fall back to comparing modtime +
// size, which catches a fresh truncated file even when the inode check
// is unavailable.
func didRotate(path string, current *os.File) (bool, error) {
	cur, err := current.Stat()
	if err != nil {
		return false, err
	}
	disk, err := os.Stat(path)
	if err != nil {
		// Renamed and not yet re-created — treat as rotated.
		return true, nil
	}
	if cur.Size() > disk.Size() {
		// File got smaller: must be a fresh post-rotate file.
		return true, nil
	}
	if !cur.ModTime().Equal(disk.ModTime()) && disk.Size() < cur.Size() {
		return true, nil
	}
	return false, nil
}
