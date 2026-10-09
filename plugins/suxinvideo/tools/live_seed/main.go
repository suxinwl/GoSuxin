// Run from the Go host root. The input file must contain vetted public HLS
// sources; the CMS applies its normal admission and media probe rules again.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	_ "github.com/suxinwl/GoSuxin/framework/contrib/drivers/mysql"
	_ "github.com/suxinwl/GoSuxin/framework/contrib/nosql/redis"
	"github.com/suxinwl/GoSuxin/internal/controller/suxinvideo"
)

func main() {
	file := flag.String("file", "", "vetted local M3U or TXT playlist")
	group := flag.String("group", "", "default channel group")
	upgradeGroups := flag.Bool("upgrade-groups", false, "apply idempotent live category ordering; no stream imports or probes")
	flag.Parse()
	if *upgradeGroups {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := suxinvideo.UpgradeLiveChannelGroups(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Live category upgrade failed")
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]bool{"upgraded": true})
		return
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "-file is required")
		os.Exit(2)
	}
	input, err := os.Open(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot open the local playlist")
		os.Exit(1)
	}
	defer input.Close()
	content, err := io.ReadAll(io.LimitReader(input, 16*1024*1024+1))
	if err != nil || len(content) > 16*1024*1024 {
		fmt.Fprintln(os.Stderr, "Playlist must be at most 16 MiB")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := suxinvideo.SeedLiveChannels(ctx, string(content), *group)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
