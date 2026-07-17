// cmd/cache.go — cache inspection and management.
package cmd

import (
	"fmt"
	"github.com/ckodex/gitlabvalet/internal/cache"
	"github.com/spf13/cobra"
)

func cacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local API response cache",
	}
	cmd.AddCommand(cacheStatsCmd(), cacheFlushCmd())
	return cmd
}

func cacheStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show cache entry counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cache.New(cfg.CachePath)
			if err != nil {
				return err
			}
			live, expired := c.Stats()
			fmt.Printf("\nCache: %s\n", colorDim(cfg.CachePath))
			fmt.Printf("  Live entries    : %d\n", live)
			fmt.Printf("  Expired (stale) : %d\n", expired)
			fmt.Printf("  Total           : %d\n\n", live+expired)
			return nil
		},
	}
}

func cacheFlushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "flush",
		Short: "Remove all cached entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cache.New(cfg.CachePath)
			if err != nil {
				return err
			}
			n := c.Flush()
			ok("Flushed %d cache entries", n)
			return nil
		},
	}
}
