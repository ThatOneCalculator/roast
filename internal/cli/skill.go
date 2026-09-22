package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bluesky-social/roast/internal/skill"
)

func newSkillCommand(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "skill",
		Short:         "Manage the Roast agent skill",
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newSkillInstallCommand(stdout), newSkillStatusCommand(stdout))
	return cmd
}

func newSkillInstallCommand(stdout io.Writer) *cobra.Command {
	var parent string
	var force bool
	cmd := &cobra.Command{
		Use:           "install",
		Short:         "Install the Roast agent skill",
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			target, err := skill.Target(parent)
			if err != nil {
				return err
			}
			before, err := skill.Inspect(target)
			if err != nil {
				return err
			}
			after, err := skill.Install(target, force)
			if err != nil {
				if len(before.Stale) > 0 {
					fmt.Fprintf(stdout, "stale files: %s\n", strings.Join(before.Stale, ", "))
				}
				return err
			}
			switch before.State {
			case skill.Symlink:
				fmt.Fprintf(stdout, "symlink (development install), left unchanged: %s\n", target)
			case skill.UpToDate:
				fmt.Fprintf(stdout, "already up to date: %s\n", target)
			default:
				fmt.Fprintf(stdout, "installed: %s (%s)\n", target, after.State)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "dir", "", "parent skills directory (default: ~/.agents/skills; /roast is appended)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite installed skill files that differ")
	return cmd
}

func newSkillStatusCommand(stdout io.Writer) *cobra.Command {
	var parent string
	cmd := &cobra.Command{
		Use:           "status",
		Short:         "Report the Roast agent skill installation status",
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			target, err := skill.Target(parent)
			if err != nil {
				return err
			}
			status, err := skill.Inspect(target)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s\t%s", status.State, status.Path)
			if len(status.Stale) > 0 {
				fmt.Fprintf(stdout, "\t%s", strings.Join(status.Stale, ","))
			}
			fmt.Fprintln(stdout)
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "dir", "", "parent skills directory (default: ~/.agents/skills; /roast is appended)")
	return cmd
}
