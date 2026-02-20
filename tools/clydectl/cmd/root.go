package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "clydectl",
	Short: "clydectl orchestrates intelligent image seeding and deployment for Clyde",
	Long:  "clydectl seeds container images across Kubernetes clusters before deploying workloads",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
