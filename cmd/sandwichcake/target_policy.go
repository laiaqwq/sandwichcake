package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/spf13/cobra"
)

// applyTagPolicy checks tags immediately before pushing. This is an
// optimistic check; registries without conditional PUT cannot guarantee CAS.
func applyTagPolicy(cmd *cobra.Command, policy, ref string) (bool, error) {
	if policy == "" || policy == "overwrite" {
		return false, nil
	}
	cl, err := client()
	if err != nil {
		return false, err
	}
	if _, err := cl.Head(cmd.Context(), ref); err != nil {
		var remoteErr *transport.Error
		if errors.As(err, &remoteErr) && remoteErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	if policy == "skip" {
		fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (tag exists)\n", ref)
		return true, nil
	}
	return false, fmt.Errorf("%s already exists (registry.onTagExists: fail)", ref)
}
