package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/laiaqwq/sandwichcake/pkg/cake"
	"github.com/laiaqwq/sandwichcake/pkg/runner"
)

func composeCmd() *cobra.Command {
	var file string
	var dryRun, yes, locked, verify bool
	var compat string
	cmd := &cobra.Command{
		Use:   "compose -f cake.yaml",
		Short: "Compose slices onto a base image",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cake.Load(file)
			if err != nil {
				return err
			}
			var lock *cake.Lock
			if locked {
				lock, err = cake.LoadLock(lockPath(file))
				if err != nil {
					return err
				}
			}
			cl, err := client()
			if err != nil {
				return err
			}
			out, err := runner.Compose(cmd.Context(), cl, c, lock, runner.Options{
				DryRun: dryRun, Yes: yes, Verify: verify, Compat: compat,
				Confirm: confirmCopy(cmd),
			})
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			for _, f := range out.Findings {
				fmt.Fprintf(w, "%s: %s\n", f.Level, f.Msg)
			}
			if out.Plan != nil && verbose {
				out.Plan.Render(w)
			}
			if dryRun {
				fmt.Fprintln(w, "dry-run complete")
				return nil
			}
			if err := out.Lock.Save(lockPath(file)); err != nil {
				return err
			}
			fmt.Fprintf(w, "pushed %s\n  digest %s\n  wrote %s\n", c.Target.Ref, out.Digest, lockPath(file))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "cake.yaml", "cake.yaml path")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and plan only")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip copy confirmation")
	cmd.Flags().BoolVar(&locked, "locked", false, "use cake.lock only (no tag resolution)")
	cmd.Flags().BoolVar(&verify, "verify", false, "download layers and scan tar headers")
	cmd.Flags().StringVar(&compat, "compat", "", "docker20: forbid zstd layers")
	return cmd
}

func planCmd() *cobra.Command {
	var file string
	var verify bool
	cmd := &cobra.Command{
		Use:   "plan -f cake.yaml",
		Short: "Show transfer plan (compose --dry-run)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cake.Load(file)
			if err != nil {
				return err
			}
			cl, err := client()
			if err != nil {
				return err
			}
			out, err := runner.Compose(cmd.Context(), cl, c, nil, runner.Options{
				DryRun: true, Yes: true, Verify: verify,
			})
			if err != nil {
				return err
			}
			out.Plan.Render(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "cake.yaml", "cake.yaml path")
	cmd.Flags().BoolVar(&verify, "verify", false, "verify layers during resolve")
	return cmd
}

func lockPath(cakeFile string) string {
	return filepath.Join(filepath.Dir(cakeFile), "cake.lock")
}

func confirmCopy(cmd *cobra.Command) func(int64) (bool, error) {
	return func(total int64) (bool, error) {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return false, fmt.Errorf("%d bytes must be copied; re-run with --yes", total)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%d bytes will be copied to the target registry. Continue? [y/N] ", total)
		var line string
		fmt.Scanln(&line)
		return strings.EqualFold(strings.TrimSpace(line), "y"), nil
	}
}
