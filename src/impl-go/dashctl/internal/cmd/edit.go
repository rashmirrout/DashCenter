package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

// editorFunc lets tests substitute the $EDITOR invocation for an in-memory
// transformation.
var editorFunc = openInEditor

func (a *Application) newEditCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "edit <kind> <name>",
		Short: "Fetch a spec, open in $EDITOR, apply on save",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ki, ok := manifest.LookupKind(args[0])
			if !ok {
				return errors.Newf(errors.CodeInvalidArgument, "edit: unknown kind %q", args[0])
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			it, err := cl.Get(ctx, rc.Namespace, ki.StoreKind, args[1])
			if err != nil {
				return err
			}
			env, err := manifest.EnvelopeFromStoredItem(ki.StoreKind, it.Namespace, it.Name, it.Generation, it.Spec, nil)
			if err != nil {
				return errors.Wrap(errors.CodeInternal, "edit", err)
			}
			orig, err := env.Marshal()
			if err != nil {
				return errors.Wrap(errors.CodeInternal, "edit", err)
			}
			edited, err := editorFunc(orig)
			if err != nil {
				return errors.Wrap(errors.CodeGeneric, "edit", err)
			}
			if bytes.Equal(orig, edited) {
				fmt.Fprintln(a.Out, "no changes")
				return nil
			}
			newEnv, err := manifest.Parse(edited)
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "edit", err)
			}
			body, err := newEnv.SpecJSON()
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "edit", err)
			}
			res, err := cl.Put(ctx, rc.Namespace, ki.StoreKind, newEnv.Metadata.Name, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s/%s updated (generation %d)\n", ki.StoreKind, newEnv.Metadata.Name, res.Generation)
			return nil
		},
	}
	return c
}

// openInEditor writes data to a temp file, spawns $EDITOR, and returns
// the post-edit bytes. Used by `dashctl edit`.
func openInEditor(data []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "dashctl-edit-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad.exe"
		} else {
			editor = "vi"
		}
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor %q exited with error: %w", editor, err)
	}
	return os.ReadFile(path)
}
